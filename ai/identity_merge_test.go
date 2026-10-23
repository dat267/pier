package ai

import (
	"strings"
	"testing"
)

// Reference: pi v1.4.0 utils/identity.ts mergeClientIdentity and
// test/identity.test.ts, adc5fa9f8 and 04a93bad8. Later defined fields win;
// omission inherits, explicit null attribution clears, and {} stays non-nil.
func TestMergeClientIdentity(t *testing.T) {
	if got := MergeClientIdentity(nil, nil); got != nil {
		t.Fatalf("undefined sources = %+v", got)
	}
	if got := MergeClientIdentity(nil, &ClientIdentity{}); got == nil {
		t.Fatal("empty identity became undefined")
	}
	base := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution}
	request := &ClientIdentity{Version: "9.9.9"}
	got := MergeClientIdentity(base, nil, request)
	if got == nil || got.UserAgent != "short" || got.Version != "9.9.9" || got.Attribution != PIAttribution {
		t.Fatalf("merged = %+v", got)
	}
	if base.Version != "" || request.UserAgent != "" || request.Attribution != nil {
		t.Fatal("merge changed a source")
	}
}

func TestMergeClientIdentityEmptyVersion(t *testing.T) {
	got := MergeClientIdentity(&ClientIdentity{Version: "9.9.9"}, &ClientIdentity{VersionSet: true})
	if got.Version != "" || !got.VersionSet {
		t.Fatalf("empty version did not override: %+v", got)
	}
	if agent := FormatUserAgent(got); !strings.HasPrefix(agent, "pi/ (") {
		t.Fatalf("empty version user agent = %q", agent)
	}
}

func TestMergeClientIdentityClearsAttribution(t *testing.T) {
	base := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution}
	cleared := MergeClientIdentity(base, &ClientIdentity{AttributionSet: true})
	if cleared == nil || cleared.Attribution != nil || !cleared.AttributionSet || cleared.UserAgent != "short" {
		t.Fatalf("cleared = %+v", cleared)
	}
	inherited := MergeClientIdentity(cleared, &ClientIdentity{Version: "9.9.9"})
	if inherited.Attribution != nil || !inherited.AttributionSet {
		t.Fatalf("null attribution was lost: %+v", inherited)
	}
	if base.Attribution != PIAttribution {
		t.Fatal("source attribution changed")
	}
}
