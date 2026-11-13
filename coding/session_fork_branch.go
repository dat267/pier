package coding

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// Branch fork: the runtime's fork, which the interactive mode's /clone and its
// fork-from-a-message both use. Upstream distinguishes two positions:
//
//	"at"     — the new session's leaf is the given entry (clone at the current
//	           position).
//	"before" — the new session's leaf is that entry's *parent*, and the entry has
//	           to be a user message, whose text goes back into the editor so the
//	           user can send it again.
//
// Both come down to the same thing: the new session holds the branch path to a
// chosen entry. That is also what fixes the leaf — the session format has no
// leaf marker, so a session's leaf is its last entry, and copying the path
// (rather than every entry, as ForkSession does) puts the chosen entry last.

// ForkSessionAtEntry creates a new session holding the branch path up to
// targetLeafID, so the new session's leaf is that entry. An empty targetLeafID
// forks an empty session (the "before the first message" case). The source may
// be an in-memory session: only its entries are read.
func ForkSessionAtEntry(source *SessionManager, targetLeafID string, targetCwd string, sessionDir string, options *NewSessionOptions) (*SessionManager, error) {
	return forkSessionAtEntry(source, targetLeafID, targetCwd, sessionDir, true, options)
}

// ForkSessionAtEntryDerived is ForkSessionAtEntry for a directory inherited from
// the source file rather than chosen as a session store: the directory places
// the fork but keeps the user's permissions instead of being hardened to 0700
// (M73). Callers pass source.SessionDirManaged() to pick between the two.
func ForkSessionAtEntryDerived(source *SessionManager, targetLeafID string, targetCwd string, sessionDir string, options *NewSessionOptions) (*SessionManager, error) {
	return forkSessionAtEntry(source, targetLeafID, targetCwd, sessionDir, false, options)
}

func forkSessionAtEntry(source *SessionManager, targetLeafID string, targetCwd string, sessionDir string, manageDir bool, options *NewSessionOptions) (*SessionManager, error) {
	var entries []SessionEntry
	if targetLeafID != "" {
		entries = source.GetBranch(targetLeafID)
		if len(entries) == 0 {
			return nil, fmt.Errorf("Invalid entry ID for forking")
		}
	}
	parentSession := source.GetSessionFile()
	var parent *string
	if parentSession != "" {
		parent = &parentSession
	}
	return writeForkedSession(targetCwd, sessionDir, manageDir, parent, entries, options)
}

// writeForkedSession writes a new session file (header, then the entries in
// order) and opens it.
func writeForkedSession(targetCwd string, sessionDir string, manageDir bool, parentSession *string, entries []SessionEntry, options *NewSessionOptions) (*SessionManager, error) {
	resolvedTargetCwd := ResolvePath(targetCwd, "", PathInputOptions{})
	dir := sessionDir
	if dir != "" {
		dir = NormalizePath(dir, PathInputOptions{})
	} else {
		dir = DefaultSessionDir(resolvedTargetCwd, "")
		manageDir = true
	}
	if manageDir {
		if err := EnsurePrivateSessionDir(dir); err != nil {
			return nil, err
		}
	}

	idSet := options != nil && (options.IDSet || options.ID != "")
	if idSet {
		if err := AssertValidSessionID(options.ID); err != nil {
			return nil, err
		}
	}
	newSessionID := UUIDv7()
	if idSet {
		newSessionID = options.ID
	}
	timestamp := sessionTimestamp()
	fileTimestamp := sessionFileTimestamp(timestamp)
	newSessionFile := filepath.Join(dir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp, newSessionID))

	newHeader := SessionHeader{
		Version:       new(CurrentSessionVersion),
		ID:            newSessionID,
		Timestamp:     timestamp,
		Cwd:           resolvedTargetCwd,
		ParentSession: parentSession,
	}
	headerLine, err := MarshalFileEntry(FileEntry{Header: &newHeader})
	if err != nil {
		return nil, err
	}

	// flag: "wx" — refuse to clobber an existing file.
	file, err := openPrivateSessionFile(newSessionFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return nil, err
	}
	writer := bufio.NewWriter(file)
	if _, err := writer.WriteString(headerLine); err != nil {
		_ = file.Close()
		return nil, err
	}
	for i := range entries {
		line, err := MarshalFileEntry(FileEntry{Entry: &entries[i]})
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if _, err := writer.WriteString(line); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	return OpenSession(newSessionFile, "", "")
}
