package cmd

import "testing"

// TestCatalogRefreshAllowedAtCreate pins the create-time catalog-refresh policy.
// Upstream refreshes the model catalogs Web at create (refreshOnCreate defaults
// to true), but the interactive mode instead defers the network refresh to the
// background pass it already runs after startup, so the first frame paints at
// store-load speed (D220). Print mode and --list-models resolve models before
// any background pass, so they keep the create-time refresh. Offline disables it
// everywhere.
func TestCatalogRefreshAllowedAtCreate(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		offline, print, list bool
		want                 bool
	}{
		{"interactive online defers", false, false, false, false},
		{"interactive offline", true, false, false, false},
		{"print online", false, true, false, true},
		{"print offline", true, true, false, false},
		{"list online", false, false, true, true},
		{"list offline", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := catalogRefreshAllowedAtCreate(tc.offline, tc.print, tc.list); got != tc.want {
				t.Fatalf("allow = %v, want %v", got, tc.want)
			}
		})
	}
}
