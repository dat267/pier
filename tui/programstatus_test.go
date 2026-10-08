package tui

import (
	"encoding/base64"
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
	}
	for _, testCase := range cases {
		testCase := testCase
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
	index := strings.Index(status, "msg=")
	if index < 0 {
		t.Fatalf("no message in %q", status[:40])
	}
	encoded := strings.TrimSuffix(status[index+len("msg="):], "\x1b\\")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) > programStatusMaxMessageBytes {
		t.Fatalf("decoded message = %d bytes, want at most %d", len(decoded), programStatusMaxMessageBytes)
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
	index := strings.Index(status, "msg=")
	if index < 0 {
		t.Fatal("no message")
	}
	encoded := strings.TrimSuffix(status[index+len("msg="):], "\x1b\\")
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
