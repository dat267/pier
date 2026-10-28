package interactive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// The corpus executes pi v1.4.0's actual offerRadiusMcpServer method and config
// writer; see coding/testdata/radius-mcp/generate.mjs. Drive the public login
// boundary here so consent, off-loop writes and reload delivery are covered.
func TestRadiusMcpOfferMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("../testdata/radius-mcp/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		Initial  *string
		Project  string
		Choice   string
		Offer    bool
		Error    bool
		Reloads  int
		Expected *string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			mcpPath := filepath.Join(app.mcpAgentDir, "mcp.json")
			if c.Initial != nil {
				if err := os.WriteFile(mcpPath, []byte(*c.Initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			projectPath := filepath.Join(app.sessionMgr.GetCwd(), ".pi", "mcp.json")
			if c.Project != "" {
				if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(projectPath, []byte(c.Project), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			provider := app.session.ModelRuntime().GetProvider("radius")
			provider.RefreshModels = func(*ai.RefreshModelsContext) error { return nil }
			provider.Auth.OAuth.Login = func(*ai.AuthInteraction) (*ai.OAuthCredential, error) {
				return &ai.OAuthCredential{OAuthCredentials: ai.OAuthCredentials{Access: "test-token", Expires: time.Now().Add(time.Hour).UnixMilli()}}, nil
			}
			authenticated, failed, reloads := false, false, 0
			app.auth.ShowStatus = func(string) { authenticated = true }
			app.auth.ShowError = func(message string) {
				failed = true
				if !strings.Contains(message, "Could not update "+mcpPath+": ") {
					t.Errorf("unexpected login error: %s", message)
				}
			}
			app.auth.OnMcpConfigured = func() { reloads++ }
			app.auth.ShowLoginDialog(context.Background(), "radius", "Radius")
			waitForCondition(t, func() bool {
				app.ui.RenderNow(true)
				return authenticated
			})
			app.offloopGroup.FlushAll()
			app.ui.RenderNow(true)
			offered := strings.Contains(strings.Join(app.editorContainer.Render(160), "\n"), "Configure Radius MCP in ")
			if offered != c.Offer {
				t.Fatalf("offer = %v, want %v", offered, c.Offer)
			}
			if offered {
				selector := app.editorContainer.Children[0].(interface{ HandleInput(string) })
				switch c.Choice {
				case "cancel":
					selector.HandleInput("\x1b")
				case "No":
					selector.HandleInput("j")
					selector.HandleInput("\r")
				default:
					selector.HandleInput("\r")
				}
				app.offloopGroup.FlushAll()
				app.ui.RenderNow(true)
			}
			if failed != c.Error || reloads != c.Reloads {
				t.Fatalf("error = %v, reloads = %d; want %v, %d", failed, reloads, c.Error, c.Reloads)
			}
			got, err := os.ReadFile(mcpPath)
			if c.Expected == nil {
				if !os.IsNotExist(err) {
					t.Fatalf("no file expected, got %q (err %v)", got, err)
				}
			} else if err != nil || string(got) != *c.Expected {
				t.Fatalf("file = %q (err %v), want %q", got, err, *c.Expected)
			}
			if c.Project != "" {
				got, err := os.ReadFile(projectPath)
				if err != nil || string(got) != c.Project {
					t.Fatalf("project config changed: %s, %v", got, err)
				}
			}
		})
	}
}
