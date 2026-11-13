package tui

// Replays the upstream program-status goldens (tui/testdata/program-status/
// generate.mjs). A report is an OSC 7501 sequence the terminal renders, so its
// pairs, control-character replacement, whitespace trimming, 2048-byte cut and
// base64 payload are compared as exact bytes.

import (
	"encoding/json"
	"os"
	"testing"
)

func TestProgramStatusMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/program-status/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Status *struct {
			State   string  `json:"state"`
			App     *string `json:"app"`
			Kind    *string `json:"kind"`
			Message *string `json:"message"`
		} `json:"status"`
		Sequence *string `json:"sequence"`
		Value    any     `json:"value"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("golden is empty")
	}
	formats, replies := 0, 0
	for _, record := range records {
		switch {
		case record.Status != nil:
			formats++
			status := ProgramStatus{State: record.Status.State}
			if record.Status.App != nil {
				status.App = *record.Status.App
			}
			if record.Status.Kind != nil {
				status.Kind = *record.Status.Kind
			}
			if record.Status.Message != nil {
				status.Message = *record.Status.Message
			}
			got := FormatProgramStatus(status)
			want, _ := record.Value.(string)
			if got != want {
				t.Errorf("FormatProgramStatus(%+v) = %q, want %q", status, got, want)
			}
		case record.Sequence != nil:
			replies++
			got := IsProgramStatusReply(*record.Sequence)
			want, _ := record.Value.(bool)
			if got != want {
				t.Errorf("IsProgramStatusReply(%q) = %v, want %v", *record.Sequence, got, want)
			}
		default:
			t.Fatalf("golden record has neither a status nor a sequence: %+v", record)
		}
	}
	if formats == 0 || replies == 0 {
		t.Fatalf("golden covered %d formats and %d replies", formats, replies)
	}
}
