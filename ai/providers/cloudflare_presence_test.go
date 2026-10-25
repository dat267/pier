package providers

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/dat267/pier/ai"
)

// Source-generated against pi v1.4.0 providers/cloudflare-auth.ts.
// Regenerate: node ai/providers/testdata/cloudflare-auth/generate.mjs ./pi > ai/providers/testdata/cloudflare-auth/fixtures.json
func TestCloudflareAuthFieldPresenceAgainstUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/cloudflare-auth/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name       string
		Kind       string
		Credential *ai.ApiKeyCredential
		Env        map[string]string
		Present    bool
		Queries    []string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			auth := CloudflareWorkersAIAuth()
			if fixture.Kind == CloudflareAuthAIGateway {
				auth = CloudflareAIGatewayAuth()
			}
			ctx := &cloudflarePresenceContext{env: fixture.Env}
			result, err := auth.Resolve(ai.AuthResolveInput{
				Ctx: ctx, Ctx2: context.Background(), Credential: fixture.Credential,
			})
			if err != nil {
				t.Fatal(err)
			}
			if (result != nil) != fixture.Present {
				t.Errorf("auth present = %v, upstream = %v", result != nil, fixture.Present)
			}
			if !slices.Equal(ctx.queries, fixture.Queries) {
				t.Errorf("ambient queries = %v, upstream = %v", ctx.queries, fixture.Queries)
			}
		})
	}
}

type cloudflarePresenceContext struct {
	env     map[string]string
	queries []string
}

func (c *cloudflarePresenceContext) Env(name string) (string, bool) {
	c.queries = append(c.queries, name)
	value, ok := c.env[name]
	return value, ok
}

func (*cloudflarePresenceContext) FileExists(string) bool { return false }
