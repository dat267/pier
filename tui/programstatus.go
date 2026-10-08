package tui

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// Port of packages/tui/src/program-status.ts: the Program Status Protocol (OSC 7501), in
// which a program tells the terminal whether it is idle, working, blocked on the user, done
// or failed. Only the root record is supported.
//
// Spec: https://www.superlogical.com/rex/docs/build/program-status

// ProgramStatus is one status report.
type ProgramStatus struct {
	// State is the reported state; ProgramStatusClear removes the status instead.
	State string
	// App is a stable program name, `[A-Za-z0-9_.+-]{1,32}`; other values are omitted.
	App string
	// Kind is what a blocked program waits for; omitted for other states.
	Kind string
	// Message is one human-readable line. Control characters become spaces and longer text
	// is cut to the spec limit.
	Message string
}

// Program status states.
const (
	ProgramStatusIdle    = "idle"
	ProgramStatusWorking = "working"
	ProgramStatusBlocked = "blocked"
	ProgramStatusDone    = "done"
	ProgramStatusError   = "error"
	// ProgramStatusClear removes the reported status.
	ProgramStatusClear = "clear"
)

// Blocked kinds.
const (
	ProgramStatusKindPermission = "permission"
	ProgramStatusKindQuestion   = "question"
	ProgramStatusKindAuth       = "auth"
)

// ProgramStatusQuery is the feature-detection query. A supporting terminal replies with the
// same body.
const ProgramStatusQuery = "\x1b]7501;?\x1b\\"

// programStatusReplyPattern matches the reply to ProgramStatusQuery. Later spec revisions
// may add pairs after the `?`.
var programStatusReplyPattern = regexp.MustCompile(`^\x1b\]7501;\?[^\x07\x1b]*(\x07|\x1b\\)$`)

// IsProgramStatusReply reports whether a terminal sequence answers ProgramStatusQuery.
func IsProgramStatusReply(sequence string) bool {
	return programStatusReplyPattern.MatchString(sequence)
}

var programStatusAppPattern = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,32}$`)

var programStatusControlCharacters = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f]+`)

// programStatusMaxMessageBytes is the decoded message limit; its base64 encoding stays under
// the 2732-byte encoded limit.
const programStatusMaxMessageBytes = 2048

func truncateUTF8Bytes(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	bytes, end := 0, 0
	for _, r := range text {
		size := len(string(r))
		if bytes+size > maxBytes {
			break
		}
		bytes += size
		end += size
	}
	return text[:end]
}

// FormatProgramStatus encodes a status report. Terminals discard reports whose text
// contains control characters, so they are replaced.
func FormatProgramStatus(status ProgramStatus) string {
	pairs := []string{"state=" + status.State}
	if status.App != "" && programStatusAppPattern.MatchString(status.App) {
		pairs = append(pairs, "app="+status.App)
	}
	if status.State == ProgramStatusBlocked && status.Kind != "" {
		pairs = append(pairs, "kind="+status.Kind)
	}
	message := truncateUTF8Bytes(strings.TrimSpace(
		programStatusControlCharacters.ReplaceAllString(status.Message, " ")), programStatusMaxMessageBytes)
	if message != "" {
		pairs = append(pairs, "msg="+base64.StdEncoding.EncodeToString([]byte(message)))
	}
	return "\x1b]7501;" + strings.Join(pairs, ":") + "\x1b\\"
}
