package coding

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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

// payloadChunkBytes is the read size of a snapshot's sequential hydration pass.
// A variable so a test can force several chunks over a small file.
var payloadChunkBytes int64 = 8 << 20

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
	if _, err := r.openLocked(); err != nil {
		return nil, err
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

// openLocked opens the file once and remembers a failure, so a session whose
// file disappeared does not retry the open on every read.
func (r *payloadReader) openLocked() (*os.File, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.file == nil {
		file, err := os.Open(r.path)
		if err != nil {
			r.err = fmt.Errorf("read session payload: %w", err)
			return nil, r.err
		}
		r.file = file
	}
	return r.file, nil
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
	if err := r.fill(entry, line); err != nil {
		return err
	}
	return nil
}

// fill decodes one retained line into a lazy entry. The line was scanned when
// the file was loaded, so the message member is read again with the same
// scanner instead of a structural JSON decode: a full snapshot hydrates every
// entry, and 72k structural decodes cost far more than the byte copy. A line
// the scanner rejects falls back to the decoder.
func (r *payloadReader) fill(entry *SessionEntry, line []byte) error {
	entry.raw = json.RawMessage(line)
	entry.lazy = false
	if meta, ok := scanSessionLine(line); ok && meta.typ == "message" && len(meta.message) > 0 {
		// meta.message is a subslice of line, so the message bytes are stored
		// once and borrowed by both members.
		entry.Message = json.RawMessage(meta.message)
		if !entry.roleKnown {
			entry.role, entry.roleKnown = roleOfMessageSpan(meta.message)
		}
		return nil
	}
	var decoded SessionEntry
	if err := decodeSessionLine(line, &decoded); err != nil {
		entry.raw = nil
		entry.lazy = true
		return fmt.Errorf("decode session entry %s: %w", entry.ID, err)
	}
	entry.Message = decoded.Message
	if len(entry.Message) == 0 {
		entry.Message = nil
	}
	if !entry.roleKnown {
		entry.role = decoded.role
		entry.roleKnown = decoded.roleKnown
	}
	return nil
}

// hydrateEntriesSeq fills every lazy entry of a snapshot with one sequential
// pass over the file, in aligned chunks, instead of one pread per entry. A
// full snapshot hydrates tens of thousands of entries; per-entry reads spent
// about a second on the 248MB session, a whole-file pass costs a fraction of
// that (D488).
func (r *payloadReader) hydrateEntriesSeq(entries []SessionEntry) error {
	type want struct {
		index  int
		offset int64
		length int32
	}
	var wants []want
	for i := range entries {
		if entries[i].lazy {
			if entries[i].lineLength <= 0 {
				return fmt.Errorf("session entry %s has no stored line range", entries[i].ID)
			}
			wants = append(wants, want{i, entries[i].lineOffset, entries[i].lineLength})
		}
	}
	if len(wants) == 0 {
		return nil
	}
	// Entries are usually already in file order (a snapshot keeps the file's
	// order, a branch follows its parents); sort only when they are not.
	ordered := true
	for i := 1; i < len(wants); i++ {
		if wants[i].offset < wants[i-1].offset {
			ordered = false
			break
		}
	}
	if !ordered {
		sort.Slice(wants, func(a, b int) bool { return wants[a].offset < wants[b].offset })
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.openLocked()
	if err != nil {
		return err
	}
	chunkSize := payloadChunkBytes
	chunk := make([]byte, chunkSize)
	var chunkStart int64 = -1
	var chunkLen int
	for _, w := range wants {
		end := w.offset + int64(w.length)
		for chunkStart < 0 || w.offset < chunkStart || end > chunkStart+int64(chunkLen) {
			chunkStart = w.offset / chunkSize * chunkSize
			n, readErr := file.ReadAt(chunk, chunkStart)
			chunkLen = n
			if readErr != nil && n == 0 {
				return fmt.Errorf("read session entry %s: %w", entries[w.index].ID, readErr)
			}
			if int64(len(chunk)) < end-chunkStart {
				// A line larger than the chunk: read it on its own.
				line := make([]byte, w.length)
				if _, readErr := file.ReadAt(line, w.offset); readErr != nil {
					return fmt.Errorf("read session entry %s: %w", entries[w.index].ID, readErr)
				}
				if err := r.fill(&entries[w.index], line); err != nil {
					return err
				}
				chunkStart = -1
				break
			}
		}
		if !entries[w.index].lazy {
			continue
		}
		// The chunk buffer is reused, so the entry gets its own copy of the line.
		line := append([]byte(nil), chunk[w.offset-chunkStart:end-chunkStart]...)
		if err := r.fill(&entries[w.index], line); err != nil {
			return err
		}
	}
	return nil
}

// hydrateCopy returns a hydrated copy of a possibly lazy entry.
func (r *payloadReader) hydrateCopy(entry *SessionEntry) (SessionEntry, error) {
	copied := *entry
	if err := r.hydrate(&copied); err != nil {
		return copied, err
	}
	return copied, nil
}

// resolveMessages decodes an entry for the session's message memo, reading a
// lazy entry's line first.
func (m *SessionManager) resolveMessages(entry *SessionEntry) []ai.Message {
	if entry.lazy {
		if copied, err := m.payloads.hydrateCopy(entry); err == nil {
			return SessionEntryToContextMessages(&copied)
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
	copied, err := payloads.hydrateCopy(entry)
	if err != nil {
		return nil
	}
	return copied.Message
}

// resolveForScan returns the entry a cache scan should read: lazy entries are
// hydrated into a copy, everything else is used as is.
func (m *SessionManager) resolveForScan(entry *SessionEntry) *SessionEntry {
	if !entry.lazy {
		return entry
	}
	copied, err := m.payloads.hydrateCopy(entry)
	if err != nil {
		return entry
	}
	return &copied
}
