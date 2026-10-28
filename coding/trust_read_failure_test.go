package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// pi v1.4.0 (4a080252), core/trust-manager.ts get/getEntry propagate
// read/parse/lock failures rather than treating them as an absent decision.
// The legacy Go signatures have no error result, so they must not silently
// return an unknown decision on failure.
func TestTrustStoreGettersSurfaceReadFailure(t *testing.T) {
	store := NewProjectTrustStore(t.TempDir())
	if err := os.WriteFile(store.TrustPath(), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"Get", "GetEntry"} {
		t.Run(method, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Error("trust-store read failure was silently ignored")
				} else if _, ok := recovered.(error); !ok {
					t.Errorf("read failure panic is not an error: %T", recovered)
				}
			}()
			if method == "Get" {
				store.Get("/project")
			} else {
				store.GetEntry("/project")
			}
		})
	}
}

// pi v1.4.0 core/project-trust.ts lets store failures escape before defaults
// or UI selection. The Go resolver must return them through its error result.
func TestTrustResolutionRejectsStoreFailures(t *testing.T) {
	for _, failure := range []string{"parse", "read", "lock"} {
		t.Run(failure, func(t *testing.T) {
			project, agent := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(project, ".pi"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, ".pi", "settings.json"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			store := NewProjectTrustStore(agent)
			if err := store.Set(project, new(false)); err != nil {
				t.Fatal(err)
			}
			options := ResolveProjectTrustedOptions{Cwd: project, TrustStore: store, DefaultProjectTrust: "always"}
			if trusted, err := ResolveProjectTrusted(options); err != nil || trusted {
				t.Fatalf("valid denial control: trusted=%v err=%v", trusted, err)
			}
			var err error
			switch failure {
			case "parse":
				err = os.WriteFile(store.TrustPath(), []byte("{"), 0600)
			case "read":
				if err = os.Remove(store.TrustPath()); err == nil {
					err = os.Mkdir(store.TrustPath(), 0700)
				}
			case "lock":
				if err = os.RemoveAll(agent); err == nil {
					err = os.WriteFile(agent, []byte("blocked"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("resolver panicked instead of returning its error: %v", recovered)
				}
			}()
			trusted, err := ResolveProjectTrusted(options)
			if trusted || err == nil {
				t.Fatalf("broken trust store: trusted=%v err=%v", trusted, err)
			}
		})
	}
}
