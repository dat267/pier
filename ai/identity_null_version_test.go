package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// The reference resolves the identity version with `identity?.version ??
// PI_AI_VERSION` (pi v1.5.0 f9a26365 utils/identity.ts:63), so a JSON null
// reads as absent and the default version is used. The port's decoder marked
// any present member as explicit, including null, and rendered "pi/ (..." with
// no version at all (D431).
func TestIdentityNullVersionUsesTheDefault(t *testing.T) {
	var nullVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":null}`), &nullVersion); err != nil {
		t.Fatal(err)
	}
	if nullVersion.VersionSet {
		t.Fatal(`{"version":null} must not read as an explicit version`)
	}
	if got := FormatUserAgent(&nullVersion); got != FormatUserAgent(&ClientIdentity{}) {
		t.Fatalf(`null version rendered %q, want the default identity's header`, got)
	}

	// An empty string is still explicit, as it is upstream.
	var emptyVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":""}`), &emptyVersion); err != nil {
		t.Fatal(err)
	}
	if !emptyVersion.VersionSet {
		t.Fatal(`{"version":""} must stay an explicit empty version`)
	}
	if ua := FormatUserAgent(&emptyVersion); !strings.HasPrefix(ua, "pi/ (") {
		t.Fatalf(`empty version rendered %q`, ua)
	}

	// A real version still wins.
	var setVersion ClientIdentity
	if err := json.Unmarshal([]byte(`{"version":"9.9.9"}`), &setVersion); err != nil {
		t.Fatal(err)
	}
	if ua := FormatUserAgent(&setVersion); !strings.HasPrefix(ua, "pi/9.9.9 (") {
		t.Fatalf("set version rendered %q", ua)
	}
}
