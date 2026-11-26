package ai

import (
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) api/cloudflare-workers-ai-system-one.ts reports a
// non-Completed run as `${LABEL} run did not complete (state: ${String(result.state)})`,
// so an object state renders "[object Object]" and an array joins with commas.
// The port embedded the raw JSON member instead, printing {"a":1} and ["x","y"].
func TestCloudflareSystemOneStateRendersLikeJavaScript(t *testing.T) {
	cases := []struct {
		name  string
		state string
		want  string
	}{
		{"string", `"Queued"`, "state: Queued"},
		{"null", `null`, "state: null"},
		{"bool", `true`, "state: true"},
		{"number", `42`, "state: 42"},
		{"fraction", `1.5`, "state: 1.5"},
		// String(1e21) is "1e+21" and String(1e-7) is "1e-7", while String(1e20)
		// and String(0.000001) stay in fixed notation (String(number) switches to
		// exponential outside [1e-6, 1e21)). A JSON literal keeps its own spelling,
		// so only the number form makes these match.
		{"exponent", `1e21`, "state: 1e+21"},
		{"small exponent", `1e-7`, "state: 1e-7"},
		{"fixed at the boundary", `1e20`, "state: 100000000000000000000"},
		{"shortest form", `0.30000000000000004`, "state: 0.30000000000000004"},
		{"trailing zeros", `1.50`, "state: 1.5"},
		{"object", `{"a":1}`, "state: [object Object]"},
		{"array", `["x","y"]`, "state: x,y"},
		{"empty array", `[]`, "state: "},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := `{"success":true,"result":{"state":` + test.state + `}}`
			_, err := parseSystemOneResponse([]byte(body), cloudflareSystemOneTransport)
			if err == nil {
				t.Fatal("expected an incomplete-run error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), test.want)
			}
		})
	}
}

// The reference interpolates `result.state` when the member is absent, which
// renders as the literal "undefined".
func TestCloudflareSystemOneAbsentStateReportsUndefined(t *testing.T) {
	_, err := parseSystemOneResponse([]byte(`{"success":true,"result":{}}`), cloudflareSystemOneTransport)
	if err == nil || !strings.Contains(err.Error(), "state: undefined") {
		t.Fatalf("error = %v, want it to contain %q", err, "state: undefined")
	}
}
