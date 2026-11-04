package coding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// D374: upstream f9a26365 packages/coding-agent/src/utils/version-check.ts
// compares package versions. Our justfile instead stamps git describe --dirty:
// at an exact tag Git emits <tag>-dirty, without a distance/hash suffix.
func TestDirtyTaggedBuildDoesNotOfferItsOwnRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", PortRepositoryURL+"/releases/tag/v0.0.44")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	for _, current := range []string{"v0.0.44-dirty", "v0.0.44-35-g0c355b20-dirty", "v0.0.44"} {
		if got := checkForLatestPortReleaseFrom(context.Background(), server.URL, current, server.Client()); got != nil {
			t.Errorf("build %q offered its own release: %+v", current, got)
		}
		if IsNewerPackageVersion("v0.0.44", current) {
			t.Errorf("explicit updater would downgrade build %q", current)
		}
	}
	for _, current := range []string{"v0.0.43-dirty", "v0.0.44-rc.1-dirty", "v0.0.43"} {
		if got := checkForLatestPortReleaseFrom(context.Background(), server.URL, current, server.Client()); got == nil {
			t.Errorf("build %q must still offer the newer release", current)
		}
	}
}
