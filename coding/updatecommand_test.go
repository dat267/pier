package coding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// withVersion sets the running version for a test (the update compares against
// it) and restores it afterwards.
func withVersion(t *testing.T, version string) {
	t.Helper()
	previous := Version
	Version = version
	t.Cleanup(func() { Version = previous })
}

// updateServer serves a release: the latest-release document, the asset and its
// checksum, under the names release.yml attaches. Deleting an entry from
// assets removes it from the document too, which is how the missing-checksum case
// is built.
type updateServer struct {
	*httptest.Server
	payload []byte

	mu     sync.Mutex
	assets map[string][]byte
	hits   map[string]int
}

func newUpdateServer(t *testing.T, tag string, payload []byte) *updateServer {
	t.Helper()
	return newUpdateServerFor(t, tag, payload, "pier-linux-amd64")
}

// newUpdateServerFor serves the release for a named asset, so a platform whose
// asset name differs (Windows appends .exe) can be exercised.
func newUpdateServerFor(t *testing.T, tag string, payload []byte, asset string) *updateServer {
	t.Helper()
	server := &updateServer{payload: payload, assets: map[string][]byte{}, hits: map[string]int{}}
	sum := sha256.Sum256(payload)
	server.assets[asset] = payload
	server.assets[asset+".sha256"] = []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n")

	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/latest" {
			server.mu.Lock()
			names := make([]string, 0, len(server.assets))
			for name := range server.assets {
				names = append(names, name)
			}
			server.mu.Unlock()
			sort.Strings(names)
			assets := make([]string, 0, len(names))
			for _, name := range names {
				assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, name, server.URL+"/"+name))
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(fmt.Sprintf(`{"tag_name":%q,"assets":[%s]}`, tag, strings.Join(assets, ","))))
			return
		}
		name := strings.TrimPrefix(request.URL.Path, "/")
		server.mu.Lock()
		body, ok := server.assets[name]
		if ok {
			server.hits[name]++
		}
		server.mu.Unlock()
		if !ok {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *updateServer) assetHits(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[name]
}

func (s *updateServer) setAsset(name string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[name] = body
}

func (s *updateServer) deleteAsset(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, name)
}

// tempTarget is a binary to replace: it exists, and it is not this test binary.
func tempTarget(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pier")
	if err := os.WriteFile(path, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// onlyFileIn reports the single file in a directory, so a leftover download shows
// up as a failure instead of passing unnoticed.
func onlyFileIn(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the target", names)
	}
	return entries[0].Name()
}

func TestRunUpdateInstallsTheReleaseAsset(t *testing.T) {
	withVersion(t, "v0.0.1")
	payload := []byte("new binary\n")
	server := newUpdateServer(t, "v9.9.9", payload)
	target := tempTarget(t)

	version, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != "v9.9.9" {
		t.Fatalf("version = %q", version)
	}
	if got := readFile(t, target); got != string(payload) {
		t.Fatalf("target = %q", got)
	}
	// A release asset arrives without its executable bit, so the update sets it.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	if name := onlyFileIn(t, filepath.Dir(target)); name != "pier" {
		t.Fatalf("leftover file %q", name)
	}
	if server.assetHits("pier-linux-amd64.sha256") != 1 {
		t.Fatalf("the checksum was not fetched: %d", server.assetHits("pier-linux-amd64.sha256"))
	}
}

func TestRunUpdateVerifiesTheChecksum(t *testing.T) {
	withVersion(t, "v0.0.1")
	server := newUpdateServer(t, "v9.9.9", []byte("new binary\n"))
	// The asset no longer matches the checksum published beside it.
	server.setAsset("pier-linux-amd64", []byte("tampered\n"))
	target := tempTarget(t)

	_, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, target); got != "old binary" {
		t.Fatalf("a mismatched download replaced the target: %q", got)
	}
	if name := onlyFileIn(t, filepath.Dir(target)); name != "pier" {
		t.Fatalf("leftover file %q", name)
	}
}

func TestRunUpdateRefusesAnUnverifiableDownload(t *testing.T) {
	withVersion(t, "v0.0.1")
	server := newUpdateServer(t, "v9.9.9", []byte("new binary\n"))
	server.deleteAsset("pier-linux-amd64.sha256")
	target := tempTarget(t)

	_, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, target); got != "old binary" {
		t.Fatalf("an unverifiable download replaced the target: %q", got)
	}
	if server.assetHits("pier-linux-amd64") != 0 {
		t.Fatal("the asset was downloaded before the checksum was checked")
	}
}

func TestRunUpdateNamesTheMissingPlatformAsset(t *testing.T) {
	withVersion(t, "v0.0.1")
	server := newUpdateServer(t, "v9.9.9", []byte("new binary\n"))
	target := tempTarget(t)

	_, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "plan9",
		GOARCH: "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "pier-plan9-amd64") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "pier-linux-amd64") {
		t.Fatalf("the error does not list what the release has: %v", err)
	}
}

func TestRunUpdateReportsAReleaseWithNoReleases(t *testing.T) {
	withVersion(t, "v0.0.1")
	missing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	target := tempTarget(t)

	_, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: missing.URL + "/latest",
		Client: missing.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "no releases") {
		t.Fatalf("err = %v", err)
	}
}

// A Windows self-update cannot overwrite the running image, so the fallback moves
// it aside first. The failing rename is injected, which is how this is exercised
// on a platform where the direct rename succeeds.
func TestRunUpdateFallsBackToQuarantine(t *testing.T) {
	withVersion(t, "v0.0.1")
	payload := []byte("new binary\n")
	server := newUpdateServer(t, "v9.9.9", payload)
	target := tempTarget(t)

	direct := true
	rename := func(oldpath, newpath string) error {
		if direct {
			direct = false
			return fmt.Errorf("simulated running image")
		}
		return os.Rename(oldpath, newpath)
	}
	version, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
		rename: rename,
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != "v9.9.9" {
		t.Fatalf("version = %q", version)
	}
	if got := readFile(t, target); got != string(payload) {
		t.Fatalf("target = %q", got)
	}
	if _, err := os.Stat(target + updateQuarantineSuffix); !os.IsNotExist(err) {
		t.Fatalf("the sidelined binary was not cleared: %v", err)
	}
}

func TestRunUpdateSkipsWhenAlreadyCurrent(t *testing.T) {
	withVersion(t, "v9.9.9")
	server := newUpdateServer(t, "v9.9.9", []byte("new binary\n"))
	target := tempTarget(t)

	version, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err != nil || version != "" {
		t.Fatalf("version = %q err = %v", version, err)
	}
	if got := readFile(t, target); got != "old binary" {
		t.Fatalf("target = %q", got)
	}
	if server.assetHits("pier-linux-amd64") != 0 {
		t.Fatal("the asset was downloaded although the version was current")
	}
}

// A build whose version cannot be ordered (a commit hash) is still updatable:
// `pier update` is explicit, unlike the startup check.
func TestRunUpdateInstallsOverAnUnorderedVersion(t *testing.T) {
	withVersion(t, "87bc513")
	payload := []byte("new binary\n")
	server := newUpdateServer(t, "v9.9.9", payload)
	target := tempTarget(t)

	version, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "linux",
		GOARCH: "amd64",
	})
	if err != nil || version != "v9.9.9" {
		t.Fatalf("version = %q err = %v", version, err)
	}
	if got := readFile(t, target); got != string(payload) {
		t.Fatalf("target = %q", got)
	}
}

func TestRunUpdateCommandParsing(t *testing.T) {
	withVersion(t, "v0.0.1")
	payload := []byte("new binary\n")
	server := newUpdateServer(t, "v9.9.9", payload)
	target := tempTarget(t)
	options := UpdateOptions{APIURL: server.URL + "/latest", Client: server.Client(), Target: target, GOOS: "linux", GOARCH: "amd64"}

	// The prompt is not an update.
	var stdout, stderr strings.Builder
	if handled, code := runUpdateCommand("pier", []string{"--help"}, &stdout, &stderr, options); handled || code != 0 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	// Its own help names the command and changes nothing.
	stdout.Reset()
	if handled, code := runUpdateCommand("pier", []string{"update", "--help"}, &stdout, &stderr, options); !handled || code != 0 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	if !strings.Contains(stdout.String(), "pier update") {
		t.Fatalf("help = %q", stdout.String())
	}
	if got := readFile(t, target); got != "old binary" {
		t.Fatalf("--help replaced the binary: %q", got)
	}
	// A successful update reports the version and names the restart.
	stdout.Reset()
	if handled, code := runUpdateCommand("pier", []string{"update"}, &stdout, &stderr, options); !handled || code != 0 {
		t.Fatalf("handled = %v code = %d stderr = %s", handled, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated to v9.9.9") || !strings.Contains(stdout.String(), "restart pier") {
		t.Fatalf("output = %q", stdout.String())
	}
	// Running it again against the same target is a no-op that says so.
	withVersion(t, "v9.9.9")
	stdout.Reset()
	if handled, code := runUpdateCommand("pier", []string{"update"}, &stdout, &stderr, options); !handled || code != 0 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	if !strings.Contains(stdout.String(), "already up to date") {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunUpdateCommandReportsFailures(t *testing.T) {
	withVersion(t, "v0.0.1")
	server := newUpdateServer(t, "v9.9.9", []byte("new binary\n"))
	server.deleteAsset("pier-linux-amd64.sha256")
	target := tempTarget(t)
	var stdout, stderr strings.Builder

	handled, code := runUpdateCommand("pier", []string{"update"}, &stdout, &stderr, UpdateOptions{
		APIURL: server.URL + "/latest", Client: server.Client(), Target: target, GOOS: "linux", GOARCH: "amd64",
	})
	if !handled || code != 1 {
		t.Fatalf("handled = %v code = %d", handled, code)
	}
	if !strings.HasPrefix(stderr.String(), "Error: ") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if got := readFile(t, target); got != "old binary" {
		t.Fatalf("target = %q", got)
	}
}

// A Windows release names its asset with the .exe suffix, which updateAssetName
// does not produce, so the updater has to try the suffix as well. The release
// workflow builds pier-windows-amd64.exe, so this is the path Windows takes.
func TestRunUpdateFindsTheWindowsExeAsset(t *testing.T) {
	withVersion(t, "v0.0.1")
	payload := []byte("MZ new binary\n")
	server := newUpdateServerFor(t, "v9.9.9", payload, "pier-windows-amd64.exe")
	target := tempTarget(t)

	version, err := RunUpdate(context.Background(), UpdateOptions{
		APIURL: server.URL + "/latest",
		Client: server.Client(),
		Target: target,
		GOOS:   "windows",
		GOARCH: "amd64",
	})
	if err != nil || version != "v9.9.9" {
		t.Fatalf("version = %q err = %v", version, err)
	}
	if got := readFile(t, target); got != string(payload) {
		t.Fatalf("target = %q", got)
	}
	if server.assetHits("pier-windows-amd64.exe") != 1 {
		t.Fatal("the .exe asset was not fetched")
	}
}
