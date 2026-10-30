package mcp

import (
	"context"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.4.0 packages/mcp/src/client.ts listResourcesPage maps
// zero validated entries to a non-nil empty resources array.
func TestClientEmptyResourcePageReturnsEmptySlice(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("resources/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resources": []any{}}, nil
	})

	page, err := client.ListResourcesPage(context.Background(), nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Resources == nil || len(page.Resources) != 0 {
		t.Fatalf("resources = %#v, want non-nil empty slice", page.Resources)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts listResourceTemplatesPage
// maps zero validated entries to a non-nil empty resourceTemplates array.
func TestClientEmptyResourceTemplatePageReturnsEmptySlice(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("resources/templates/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resourceTemplates": []any{}}, nil
	})

	page, err := client.ListResourceTemplatesPage(context.Background(), nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.ResourceTemplates == nil || len(page.ResourceTemplates) != 0 {
		t.Fatalf("resourceTemplates = %#v, want non-nil empty slice", page.ResourceTemplates)
	}
}
