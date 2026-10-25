package providers

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 providers/cloudflare-auth.ts resolveValue checks signal after each
// ambient lookup. Cancellation during a key lookup must neither return auth
// nor query the account/gateway after the lookup completes.
func TestCloudflareAuthStopsAfterCancelledEnvLookup(t *testing.T) {
	for _, auth := range []*ai.ApiKeyAuth{CloudflareWorkersAIAuth(), CloudflareAIGatewayAuth()} {
		ctx, cancel := context.WithCancel(context.Background())
		ambient := &cloudflareCancellingContext{cancel: cancel}
		result, err := auth.Resolve(ai.AuthResolveInput{Ctx: ambient, Ctx2: ctx})
		cancel()
		if !errors.Is(err, context.Canceled) || result != nil {
			t.Fatalf("cancelled lookup: result present = %v, error = %v", result != nil, err)
		}
		if !slices.Equal(ambient.queries, []string{CloudflareAPIKeyEnv}) {
			t.Fatalf("lookups continued after cancellation: %v", ambient.queries)
		}
	}
}

type cloudflareCancellingContext struct {
	cancel  context.CancelFunc
	queries []string
}

func (c *cloudflareCancellingContext) Env(name string) (string, bool) {
	c.queries = append(c.queries, name)
	c.cancel()
	return "fixture-value", true
}

func (*cloudflareCancellingContext) FileExists(string) bool { return false }
