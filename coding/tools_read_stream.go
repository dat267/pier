package coding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Port of the streaming text-read path in pi v1.5.2 (b4afdddf)
// core/tools/read.ts (selectLinesFromStream): only the lines a read shows are
// kept in memory, so the cost of reading a file follows the output, not the
// file. The whole-file path stays for ranges whose JavaScript array slicing
// defines what they select (fractional or negative offset/limit).

// readSelection is the lines a read shows, with the file's line count. Line
// counts follow text.split("\n"), including a trailing empty line.
type readSelection struct {
	totalFileLines int
	truncation     TruncationResult
	// firstLineBytes is the size of the first selected line, for the notice when
	// that line alone exceeds the byte limit.
	firstLineBytes int
}

// isStreamableReadRange reports whether offset and limit select lines with plain
// integers, the only ranges the streaming selector handles. Other values keep
// the whole-file path, for example limit -1 as every line but the last.
func isStreamableReadRange(offset, limit *float64) bool {
	isInteger := func(value float64) bool { return value == float64(int64(value)) }
	if offset != nil && !isInteger(*offset) {
		return false
	}
	return limit == nil || (isInteger(*limit) && *limit >= 0)
}

// selectTextLinesFromBuffer selects lines from a whole file in memory: for
// unusual ranges, where JavaScript array slicing defines the selection.
func selectTextLinesFromBuffer(data []byte, startLine float64, limit *float64) readSelection {
	allLines := strings.Split(DecodeUTF8WithReplacement(data), "\n")
	sliceStart := jsArraySliceIndex(startLine, len(allLines))
	var selectedContent string
	if limit != nil {
		endLine := min(startLine+*limit, float64(len(allLines)))
		sliceEnd := max(jsArraySliceIndex(endLine, len(allLines)), sliceStart)
		selectedContent = strings.Join(allLines[sliceStart:sliceEnd], "\n")
	} else {
		selectedContent = strings.Join(allLines[sliceStart:], "\n")
	}
	truncation := TruncateHead(selectedContent, TruncationOptions{})
	firstLineBytes := 0
	if truncation.FirstLineExceedsLimit && sliceStart < len(allLines) {
		firstLineBytes = len(allLines[sliceStart])
	}
	return readSelection{totalFileLines: len(allLines), truncation: truncation, firstLineBytes: firstLineBytes}
}

// selectTextLinesFromStream selects lines while reading the file in chunks.
type lineSelector struct {
	startLine int
	// endLine is exclusive; readUnlimitedEnd means every line from startLine.
	endLine int

	decoder utf8TextDecoder

	storedLines   []string
	storedBytes   int
	storedPastCap bool

	line           int
	selectedLines  int
	selectedBytes  int
	firstLineBytes int
	lastLineEmpty  bool

	pieces      []string
	storePieces bool
	lineBytes   int
	lineHasText bool
}

const readUnlimitedEnd = int(^uint(0) >> 1)

func newLineSelector(startLine int, limit *int) *lineSelector {
	end := readUnlimitedEnd
	if limit != nil {
		end = startLine + *limit
	}
	return &lineSelector{startLine: startLine, endLine: end, storePieces: true}
}

func (s *lineSelector) selected() bool { return s.line >= s.startLine && s.line < s.endLine }

func (s *lineSelector) appendToLine(piece string) {
	s.lineHasText = true
	if !s.storePieces {
		return
	}
	// A line over the byte cap is never shown, and a first line over it is only
	// measured: never store all of a 1 GB line.
	if s.lineBytes <= DefaultMaxBytes {
		s.pieces = append(s.pieces, piece)
	}
	s.lineBytes += len(piece)
}

func (s *lineSelector) finishLine() {
	if s.storePieces {
		if s.selectedLines == 0 {
			s.firstLineBytes = s.lineBytes
		}
		// Storing continues one line past the caps: TruncateHead ignores a
		// trailing empty line, so the line after the cap must be there for it to
		// decide truncation as for the full text.
		if len(s.storedLines) > DefaultMaxLines || s.storedBytes > DefaultMaxBytes {
			s.storedPastCap = true
		}
		text := strings.Join(s.pieces, "")
		s.storedBytes += len(text)
		if len(s.storedLines) > 0 {
			s.storedBytes++
		}
		s.storedLines = append(s.storedLines, text)
	}
	s.selectedLines++
	s.lastLineEmpty = !s.lineHasText
	s.storePieces = !s.storedPastCap
	s.pieces = nil
	s.lineBytes = 0
	s.lineHasText = false
}

func (s *lineSelector) selectText(text string) {
	position := 0
	// Start of the selected text in this chunk, measured as a whole.
	selectionStart := -1
	for {
		selected := s.selected()
		if selected && selectionStart == -1 {
			selectionStart = position
		}
		offset := strings.IndexByte(text[position:], '\n')
		end := len(text)
		if offset >= 0 {
			end = position + offset
		}
		if selected && end > position {
			s.appendToLine(text[position:end])
		}
		if offset < 0 {
			break
		}
		if selected {
			s.finishLine()
			if s.line+1 == s.endLine {
				s.selectedBytes += len(text[selectionStart:end])
				selectionStart = -1
			}
		}
		s.line++
		position = end + 1
	}
	if selectionStart != -1 {
		s.selectedBytes += len(text[selectionStart:])
	}
}

func (s *lineSelector) finish() readSelection {
	if s.selected() {
		s.finishLine()
	}
	truncation := TruncateHead(strings.Join(s.storedLines, "\n"), TruncationOptions{})
	totalLines := 0
	if s.selectedBytes != 0 {
		totalLines = s.selectedLines
		if s.lastLineEmpty {
			totalLines--
		}
	}
	truncation.TotalLines = totalLines
	truncation.TotalBytes = s.selectedBytes
	return readSelection{totalFileLines: s.line + 1, truncation: truncation, firstLineBytes: s.firstLineBytes}
}

// selectTextLinesFromStream reads path in chunks and returns the lines a read
// shows, stopping when the context is cancelled.
func selectTextLinesFromStream(path string, startLine int, limit *int, ctx context.Context) (readSelection, error) {
	file, err := os.Open(path)
	if err != nil {
		return readSelection{}, err
	}
	defer file.Close()
	stats, err := file.Stat()
	if err != nil {
		return readSelection{}, err
	}
	selector := newLineSelector(startLine, limit)
	// Read a regular file up to the size it had when opened, the way fs.readFile
	// does, so text appended during the read is not counted and a growing file
	// still ends. A file reporting size 0 (such as /proc files) and other file
	// types read until EOF.
	var reader io.Reader = file
	if stats.Mode().IsRegular() && stats.Size() > 0 {
		reader = io.LimitReader(file, stats.Size())
	}
	buffer := make([]byte, 64<<10)
	for {
		if ctxErrOf(ctx) != nil {
			return readSelection{}, fmt.Errorf("Operation aborted")
		}
		read, readErr := reader.Read(buffer)
		if read > 0 {
			selector.selectText(selector.decoder.decode(buffer[:read], false))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readSelection{}, readErr
		}
	}
	selector.selectText(selector.decoder.decode(nil, true))
	return selector.finish(), nil
}
