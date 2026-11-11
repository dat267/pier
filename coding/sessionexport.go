package coding

import (
	"os"
	"path/filepath"
	"strings"
)

// Port of core/session-export.ts: serializing the current branch as JSONL.

// SerializeSessionBranch renders the session header followed by every entry on
// the current branch, re-chained parent ids, plus optional export-only trailing
// entries.
func SerializeSessionBranch(sessionManager *SessionManager, createTrailingEntries func(parentID *string, timestamp string) []*SessionEntry) string {
	serialized, _ := serializeSessionBranch(sessionManager, createTrailingEntries)
	return serialized
}

// serializeSessionBranch retains serialization failures for file exporters;
// the legacy string-only helper above keeps its empty-string failure contract.
func serializeSessionBranch(sessionManager *SessionManager, createTrailingEntries func(parentID *string, timestamp string) []*SessionEntry) (string, error) {
	timestamp := sessionTimestamp()
	version := CurrentSessionVersion
	header := SessionHeader{
		Version:   &version,
		ID:        sessionManager.GetSessionID(),
		Timestamp: timestamp,
		Cwd:       sessionManager.GetCwd(),
	}
	// MarshalFileEntry writes the `type` discriminator (the header has no
	// `type` struct field, so marshalling it directly would produce an invalid
	// session file).
	headerLine, err := MarshalFileEntry(FileEntry{Header: &header})
	if err != nil {
		return "", err
	}
	lines := []string{strings.TrimRight(headerLine, "\n")}
	var parentID *string
	for _, entry := range sessionManager.GetBranch("") {
		// A fresh pointer per entry: they all re-chain to the previous entry.
		current := parentID
		// Re-chaining parses and rebuilds the entry's stored line. On a linear
		// branch the stored parentId is already the previous entry's id, so only
		// the first entry (and a branch's cut point) actually changes. Both-nil
		// still re-chains: the stored line may omit the member, and upstream's
		// assignment writes it.
		if entry.ParentID == nil || current == nil || *entry.ParentID != *current {
			entry.setParentID(current)
		}
		line, err := MarshalFileEntry(FileEntry{Entry: &entry})
		if err != nil {
			return "", err
		}
		lines = append(lines, strings.TrimRight(line, "\n"))
		parentID = &entry.ID
	}
	if createTrailingEntries != nil {
		for _, entry := range createTrailingEntries(parentID, timestamp) {
			line, err := MarshalFileEntry(FileEntry{Entry: entry})
			if err != nil {
				return "", err
			}
			lines = append(lines, strings.TrimRight(line, "\n"))
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// ExportSessionToJsonl writes the current branch to a JSONL file and returns
// the resolved path.
func ExportSessionToJsonl(sessionManager *SessionManager, outputPath string, createTrailingEntries func(parentID *string, timestamp string) []*SessionEntry) (string, error) {
	filePath := ResolvePath(outputPath, "", PathInputOptions{})
	if outputPath == "" {
		filePath = ResolvePath("session-"+sessionFileTimestamp(sessionTimestamp())+".jsonl", "", PathInputOptions{})
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	serialized, err := serializeSessionBranch(sessionManager, createTrailingEntries)
	if err != nil {
		return "", err
	}
	if err := writePrivateSessionFile(filePath, []byte(serialized)); err != nil {
		return "", err
	}
	return filePath, nil
}

// ExportToJsonl exports the current session branch (upstream exportToJsonl).
func (s *AgentSession) ExportToJsonl(outputPath string) (string, error) {
	return ExportSessionToJsonl(s.Sessions, outputPath, nil)
}
