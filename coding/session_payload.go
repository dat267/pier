package coding

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/dat267/pier/ai"
)

// Session payloads stay on disk.
//
// A loaded message entry is an index shell: type, id, parent, timestamp, and
// its JSON role, plus the byte range of its line in the session file. Its
// message bytes are not retained, because a real session holds one message per
// turn and a long one reaches hundreds of megabytes while only the entries in
// the compaction window are ever read. Reading a line back is a pread of the
// exact range the loader recorded, so it needs no rescan of the file (D488).
//
// The shells are immutable: hydration always produces a copy, so a projection,
// a footer read, or a replay cannot race with another reader over the bytes.

// payloadReader reads lazy entries' lines back from their session file. The
// file is opened on first use and kept open: resolutions are clustered (a
// projection decodes its window at once, an export walks every entry) and
// ReadAt needs no seeking, so one descriptor serves all of them. The file is
// always opened read-only, which leaves appends and renames to the writer.
type payloadReader struct {
	mu   sync.Mutex
	path string
	file *os.File
	err  error
}

func newPayloadReader(path string) *payloadReader {
	return &payloadReader{path: path}
}

// line returns the entry's original line bytes, without its newline.
func (r *payloadReader) line(entry *SessionEntry) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.file == nil {
		file, err := os.Open(r.path)
		if err != nil {
			// Remember the failure: a session whose file disappeared cannot
			// resolve anything, and every caller would retry the open.
			r.err = fmt.Errorf("read session payload: %w", err)
			return nil, r.err
		}
		r.file = file
	}
	if entry.lineLength <= 0 {
		return nil, fmt.Errorf("session entry %s has no stored line range", entry.ID)
	}
	line := make([]byte, entry.lineLength)
	if _, err := r.file.ReadAt(line, entry.lineOffset); err != nil {
		return nil, fmt.Errorf("read session entry %s: %w", entry.ID, err)
	}
	return line, nil
}

// close releases the descriptor. A closed reader reopens on the next
// resolution, so closing is only about not holding a file open while the
// session file is replaced.
func (r *payloadReader) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// hydrate fills a lazy entry's retained line and message from the file. It
// requires a private entry value: callers hydrate copies, never the manager's
// canonical entries.
func (r *payloadReader) hydrate(entry *SessionEntry) error {
	if !entry.lazy {
		return nil
	}
	line, err := r.line(entry)
	if err != nil {
		return err
	}
	var decoded SessionEntry
	if err := decodeSessionLine(line, &decoded); err != nil {
		return fmt.Errorf("decode session entry %s: %w", entry.ID, err)
	}
	entry.raw = json.RawMessage(line)
	entry.Message = decoded.Message
	if len(entry.Message) == 0 {
		entry.Message = nil
	}
	if !entry.roleKnown {
		entry.role = decoded.role
		entry.roleKnown = decoded.roleKnown
	}
	entry.lazy = false
	return nil
}

// hydrateCopy returns a hydrated copy of a possibly lazy entry.
func (r *payloadReader) hydrateCopy(entry *SessionEntry) (SessionEntry, error) {
	copy := *entry
	if err := r.hydrate(&copy); err != nil {
		return copy, err
	}
	return copy, nil
}

// hydrateEntries fills every lazy entry of a materialized slice in place. The
// slice holds copies, so the manager's canonical entries stay shells.
func (r *payloadReader) hydrateEntries(entries []SessionEntry) error {
	if r == nil {
		return nil
	}
	for i := range entries {
		if err := r.hydrate(&entries[i]); err != nil {
			return err
		}
	}
	return nil
}

// resolveMessages decodes an entry for the session's message memo, reading a
// lazy entry's line first.
func (m *SessionManager) resolveMessages(entry *SessionEntry) []ai.Message {
	if entry.lazy {
		if copy, err := m.payloads.hydrateCopy(entry); err == nil {
			return SessionEntryToContextMessages(&copy)
		}
		// A payload that cannot be read yields no messages rather than a
		// mis-decoded one. The write diagnostics carry the failure's cousin
		// for writes; a read failure here surfaces as an empty entry in the
		// projection, which the tests for a missing file pin.
		return nil
	}
	return SessionEntryToContextMessages(entry)
}

// Close releases the payload reader's descriptor. A later read reopens the
// file, so closing is only about not holding it open across a replacement.
func (m *SessionManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closePayloadsLocked()
}

// closePayloadsLocked closes the reader under m.mu.
func (m *SessionManager) closePayloadsLocked() {
	m.payloads.close()
}

// EntryMessage returns one entry's message bytes, reading a lazily loaded
// entry's line from the file. Callers that need bytes for a handful of entries
// (a replay scan, a notice) use this instead of hydrating a whole branch.
func (m *SessionManager) EntryMessage(entry *SessionEntry) json.RawMessage {
	if entry == nil {
		return nil
	}
	m.mu.Lock()
	payloads := m.payloads
	m.mu.Unlock()
	if !entry.lazy || payloads == nil {
		return entry.Message
	}
	copy, err := payloads.hydrateCopy(entry)
	if err != nil {
		return nil
	}
	return copy.Message
}

// resolveForScan returns the entry a cache scan should read: lazy entries are
// hydrated into a copy, everything else is used as is.
func (m *SessionManager) resolveForScan(entry *SessionEntry) *SessionEntry {
	if !entry.lazy {
		return entry
	}
	copy, err := m.payloads.hydrateCopy(entry)
	if err != nil {
		return entry
	}
	return &copy
}
