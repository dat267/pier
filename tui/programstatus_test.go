package tui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Ported from the format rules in packages/tui/src/program-status.ts.
func TestFormatProgramStatus(t *testing.T) {
	cases := []struct {
		name   string
		status ProgramStatus
		want   string
	}{
		{
			name:   "idle with the app name",
			status: ProgramStatus{State: ProgramStatusIdle, App: "pier"},
			want:   "\x1b]7501;state=idle:app=pier\x1b\\",
		},
		{
			name:   "clear carries no app or message",
			status: ProgramStatus{State: ProgramStatusClear},
			want:   "\x1b]7501;state=clear\x1b\\",
		},
		{
			name:   "working with the session name",
			status: ProgramStatus{State: ProgramStatusWorking, App: "pier", Message: "my session"},
			want:   "\x1b]7501;state=working:app=pier:msg=" + base64.StdEncoding.EncodeToString([]byte("my session")) + "\x1b\\",
		},
		{
			name:   "blocked carries its kind",
			status: ProgramStatus{State: ProgramStatusBlocked, App: "pier", Kind: ProgramStatusKindAuth, Message: "Log in to Anthropic"},
			want:   "\x1b]7501;state=blocked:app=pier:kind=auth:msg=" + base64.StdEncoding.EncodeToString([]byte("Log in to Anthropic")) + "\x1b\\",
		},
		{
			name:   "kind is only reported for blocked",
			status: ProgramStatus{State: ProgramStatusDone, App: "pier", Kind: ProgramStatusKindAuth},
			want:   "\x1b]7501;state=done:app=pier\x1b\\",
		},
		{
			name:   "an app name outside the spec pattern is dropped",
			status: ProgramStatus{State: ProgramStatusIdle, App: "not a valid app name!"},
			want:   "\x1b]7501;state=idle\x1b\\",
		},
		{
			// A terminal discards a report whose text carries control characters.
			name:   "control characters become spaces and the message is trimmed",
			status: ProgramStatus{State: ProgramStatusError, Message: "  boom\n\tnow  "},
			want:   "\x1b]7501;state=error:msg=" + base64.StdEncoding.EncodeToString([]byte("boom now")) + "\x1b\\",
		},
		{
			name:   "a blank message is omitted",
			status: ProgramStatus{State: ProgramStatusIdle, Message: "   "},
			want:   "\x1b]7501;state=idle\x1b\\",
		},
		{
			// Upstream trims with String.trim, so U+FEFF is blank.
			name:   "a U+FEFF message is trimmed away",
			status: ProgramStatus{State: ProgramStatusIdle, Message: "\uFEFF"},
			want:   "\x1b]7501;state=idle\x1b\\",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := FormatProgramStatus(testCase.status); got != testCase.want {
				t.Fatalf("got  %q\nwant %q", got, testCase.want)
			}
		})
	}
}

// The decoded message limit keeps the encoded report inside the spec's 2732-byte limit.
func TestFormatProgramStatusTruncatesTheMessage(t *testing.T) {
	status := FormatProgramStatus(ProgramStatus{State: ProgramStatusError, Message: strings.Repeat("a", 4000)})
	_, after, ok := strings.Cut(status, "msg=")
	if !ok {
		t.Fatalf("no message in %q", status[:40])
	}
	encoded := strings.TrimSuffix(after, "\x1b\\")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	// program-status.ts MAX_MESSAGE_BYTES is 2048; the literal keeps the bound
	// pinned when the port's constant changes.
	if len(decoded) > 2048 {
		t.Fatalf("decoded message = %d bytes, want at most 2048", len(decoded))
	}
	// The spec's 2732-byte limit applies to the encoded message: 2048 decoded bytes encode
	// to exactly 2732 base64 characters.
	if len(encoded) > 2732 {
		t.Fatalf("encoded message = %d bytes, want at most 2732", len(encoded))
	}
}

// A multi-byte rune is never cut in half by the byte limit.
func TestFormatProgramStatusKeepsRunesWhole(t *testing.T) {
	status := FormatProgramStatus(ProgramStatus{State: ProgramStatusError, Message: strings.Repeat("é", 2000)})
	_, after, ok := strings.Cut(status, "msg=")
	if !ok {
		t.Fatal("no message")
	}
	encoded := strings.TrimSuffix(after, "\x1b\\")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(decoded), "é") || strings.Contains(string(decoded), "\ufffd") {
		t.Fatalf("decoded a broken rune: %q", string(decoded)[:8])
	}
}

// The reply to ProgramStatusQuery is the query body itself, with either terminator.
func TestIsProgramStatusReply(t *testing.T) {
	cases := []struct {
		sequence string
		want     bool
	}{
		{sequence: "\x1b]7501;?\x1b\\", want: true},
		{sequence: "\x1b]7501;?\x07", want: true},
		// Later spec revisions may add pairs after the `?`.
		{sequence: "\x1b]7501;?version=2\x1b\\", want: true},
		{sequence: "\x1b]7501;?version=2\x07", want: true},
		{sequence: ProgramStatusQuery, want: true},
		{sequence: "\x1b]7501;state=working\x1b\\", want: false},
		{sequence: "\x1b]7501;?\x1b\\junk", want: false},
		{sequence: "\x1b[?1;2c", want: false},
		{sequence: "", want: false},
	}
	for _, testCase := range cases {
		if got := IsProgramStatusReply(testCase.sequence); got != testCase.want {
			t.Errorf("IsProgramStatusReply(%q) = %v, want %v", testCase.sequence, got, testCase.want)
		}
	}
}

// Replays the upstream program-status goldens (tui/testdata/program-status/
// generate.mjs). A report is an OSC 7501 sequence the terminal renders, so its
// pairs, control-character replacement, whitespace trimming, 2048-byte cut and
// base64 payload are compared as exact bytes.

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
