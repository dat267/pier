package tui

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Replays the AnsiCodeTracker goldens generated from the pinned reference: the
// active codes re-emitted at the next line (through wrapTextWithAnsi) and the
// active background code, over attributes, standard colours, 256/RGB colours,
// combined and malformed fields, OSC 8 open/close in both terminators, and a
// truncated CSI.
func TestAnsiTrackerMatchesThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/ansi-tracker/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Label      string   `json:"label"`
		Input      string   `json:"input"`
		Wrap       []string `json:"wrap"`
		Background string   `json:"background"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty ansi-tracker golden")
	}
	for _, testCase := range cases {
		if got := WrapTextWithAnsi(testCase.Input+"\nnext", 20); !reflect.DeepEqual(got, testCase.Wrap) {
			t.Errorf("%s: wrap = %q, want %q", testCase.Label, got, testCase.Wrap)
		}
		if got := GetActiveBackgroundAnsi(testCase.Input); got != testCase.Background {
			t.Errorf("%s: background = %q, want %q", testCase.Label, got, testCase.Background)
		}
	}
}
