package durable

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// BinaryReader ports the positional reader from pi v1.1.0 durable/src/env/index.ts.
type BinaryReader interface {
	Info(ctx context.Context) (*FileInfo, error)
	Read(offset int64, length int, ctx context.Context) ([]byte, error)
	ScanLines(startLine int64, endLine *int64, ctx context.Context) (LineScan, error)
	Close(ctx context.Context) error
}

// LineScan is the position and decoded size of one selected line range.
type LineScan struct {
	Newlines       int64
	Start          int64
	End            int64
	FirstLineEnd   int64
	LastLineStart  int64
	SelectedBytes  int64
	FirstLineBytes int64
}

type osBinaryReader struct {
	path   string
	file   *os.File
	mu     sync.Mutex
	closed bool
}

// OpenBinaryReader opens a regular file for bounded positional reads.
func (e *OSFileSystem) OpenBinaryReader(path string, ctx context.Context) (BinaryReader, error) {
	if err := checkFileContext(ctx); err != nil {
		return nil, err
	}
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, toFileError(err, resolved)
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, &FileError{Code: FileErrorIsDirectory, Message: "Path is a directory", Path: resolved}
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, &FileError{Code: FileErrorInvalid, Message: "Path is not a regular file", Path: resolved}
	}
	return &osBinaryReader{path: resolved, file: file}, nil
}

func (r *osBinaryReader) Info(ctx context.Context) (*FileInfo, error) {
	if err := checkFileContext(ctx); err != nil {
		return nil, err
	}
	if err := r.assertOpen(); err != nil {
		return nil, err
	}
	info, err := r.file.Stat()
	if err != nil {
		return nil, toFileError(err, r.path)
	}
	return fileInfoFromStat(r.path, info)
}

func (r *osBinaryReader) Read(offset int64, length int, ctx context.Context) ([]byte, error) {
	if offset < 0 || length < 0 {
		return nil, &FileError{Code: FileErrorInvalid, Message: "Invalid binary read range", Path: r.path}
	}
	if length == 0 {
		if err := checkFileContext(ctx); err != nil {
			return nil, err
		}
		if err := r.assertOpen(); err != nil {
			return nil, err
		}
		return []byte{}, nil
	}
	buffer := make([]byte, length)
	count, err := r.readInto(offset, buffer, ctx)
	if err != nil {
		return nil, err
	}
	return buffer[:count], nil
}

func (r *osBinaryReader) readInto(offset int64, buffer []byte, ctx context.Context) (int, error) {
	if err := checkFileContext(ctx); err != nil {
		return 0, err
	}
	if offset < 0 {
		return 0, &FileError{Code: FileErrorInvalid, Message: "Invalid binary read range", Path: r.path}
	}
	if err := r.assertOpen(); err != nil {
		return 0, err
	}
	count, err := r.file.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, toFileError(err, r.path)
	}
	return count, nil
}

func (r *osBinaryReader) ScanLines(startLine int64, endLine *int64, ctx context.Context) (LineScan, error) {
	if startLine < 0 || (endLine != nil && *endLine <= startLine) {
		return LineScan{}, &FileError{Code: FileErrorInvalid, Message: "Invalid line range", Path: r.path}
	}
	info, err := r.Info(ctx)
	if err != nil {
		return LineScan{}, err
	}
	size := info.Size
	scan := LineScan{Start: size, End: size, FirstLineEnd: size, LastLineStart: size}
	firstBytes, err := r.Read(0, 3, ctx)
	if err != nil {
		return LineScan{}, err
	}
	bom := len(firstBytes) == 3 && firstBytes[0] == 0xef && firstBytes[1] == 0xbb && firstBytes[2] == 0xbf

	position := int64(0)
	line := int64(0)
	started, ended := false, false
	var selectedDecoder, firstDecoder *rangeUTF8Decoder
	beginSelection := func(at int64) {
		scan.Start = at
		scan.LastLineStart = at
		started = true
		selectedDecoder = newRangeUTF8Decoder()
		firstDecoder = newRangeUTF8Decoder()
	}
	if startLine == 0 {
		beginSelection(0)
	}
	feedSelected := func(data []byte, at int64) error {
		if len(data) == 0 || selectedDecoder == nil {
			return nil
		}
		decoded, err := selectedDecoder.Feed(skipLeadingBOM(data, at, bom))
		scan.SelectedBytes += int64(len(decoded))
		return err
	}
	feedFirst := func(data []byte, at int64) error {
		if len(data) == 0 || firstDecoder == nil {
			return nil
		}
		decoded, err := firstDecoder.Feed(skipLeadingBOM(data, at, bom))
		scan.FirstLineBytes += int64(len(decoded))
		return err
	}
	finishSelected := func() error {
		if selectedDecoder == nil {
			return nil
		}
		decoded, err := selectedDecoder.Finish()
		scan.SelectedBytes += int64(len(decoded))
		selectedDecoder = nil
		return err
	}
	finishFirst := func() error {
		if firstDecoder == nil {
			return nil
		}
		decoded, err := firstDecoder.Finish()
		scan.FirstLineBytes += int64(len(decoded))
		firstDecoder = nil
		return err
	}

	const scanChunkSize = 64 * 1024
	buffer := make([]byte, scanChunkSize)
	for position < size {
		length := scanChunkSize
		if remaining := size - position; remaining < int64(length) {
			length = int(remaining)
		}
		count, err := r.readInto(position, buffer[:length], ctx)
		if err != nil {
			return LineScan{}, err
		}
		chunk := buffer[:count]
		if len(chunk) == 0 {
			break
		}
		from := 0
		for from < len(chunk) {
			relative := indexByte(chunk, from, '\n')
			if relative < 0 {
				at := position + int64(from)
				if started && !ended {
					if err := feedSelected(chunk[from:], at); err != nil {
						return LineScan{}, err
					}
				}
				if line == startLine {
					if err := feedFirst(chunk[from:], at); err != nil {
						return LineScan{}, err
					}
				}
				break
			}
			newline := from + relative
			at := position + int64(newline)
			lineBytes := chunk[from:newline]
			if started && !ended {
				if err := feedSelected(lineBytes, position+int64(from)); err != nil {
					return LineScan{}, err
				}
			}
			if line == startLine {
				if err := feedFirst(lineBytes, position+int64(from)); err != nil {
					return LineScan{}, err
				}
				scan.FirstLineEnd = at
				if err := finishFirst(); err != nil {
					return LineScan{}, err
				}
			}
			lastSelectedLine := endLine != nil && line == *endLine-1
			if started && !ended {
				if lastSelectedLine {
					scan.End = at
					ended = true
					if err := finishSelected(); err != nil {
						return LineScan{}, err
					}
				} else if err := feedSelected(chunk[newline:newline+1], at); err != nil {
					return LineScan{}, err
				}
			}
			line++
			lineStart := at + 1
			if line == startLine {
				beginSelection(lineStart)
			}
			if started && !ended {
				scan.LastLineStart = lineStart
			}
			from = newline + 1
		}
		position += int64(len(chunk))
	}
	scan.Newlines = line
	if line == startLine && firstDecoder != nil {
		scan.FirstLineEnd = size
		if err := finishFirst(); err != nil {
			return LineScan{}, err
		}
	}
	if started && !ended {
		scan.End = size
		if err := finishSelected(); err != nil {
			return LineScan{}, err
		}
	}
	return scan, nil
}

func (r *osBinaryReader) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	if err := r.file.Close(); err != nil {
		return toFileError(err, r.path)
	}
	return nil
}

func (r *osBinaryReader) assertOpen() error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return &FileError{Code: FileErrorInvalid, Message: "Binary reader is closed", Path: r.path}
	}
	return nil
}

func checkFileContext(ctx context.Context) error {
	if ctx != nil {
		return ctx.Err()
	}
	return nil
}

func skipLeadingBOM(data []byte, offset int64, bom bool) []byte {
	if !bom || offset >= 3 {
		return data
	}
	skip := int(3 - offset)
	if skip >= len(data) {
		return nil
	}
	return data[skip:]
}

func indexByte(data []byte, from int, target byte) int {
	for index := from; index < len(data); index++ {
		if data[index] == target {
			return index - from
		}
	}
	return -1
}

// rangeUTF8Decoder counts decoded UTF-8 output while retaining at most one incomplete rune.
type rangeUTF8Decoder struct {
	decoder     transform.Transformer
	pending     []byte
	source      []byte
	destination []byte
}

func newRangeUTF8Decoder() *rangeUTF8Decoder {
	return &rangeUTF8Decoder{decoder: textunicode.UTF8.NewDecoder()}
}

func (d *rangeUTF8Decoder) Feed(data []byte) ([]byte, error) { return d.decode(data, false) }
func (d *rangeUTF8Decoder) Finish() ([]byte, error)          { return d.decode(nil, true) }

func (d *rangeUTF8Decoder) decode(data []byte, atEOF bool) ([]byte, error) {
	d.source = append(d.source[:0], d.pending...)
	d.source = append(d.source, data...)
	d.pending = d.pending[:0]
	needed := len(d.source)*3 + 3
	if cap(d.destination) < needed {
		d.destination = make([]byte, needed)
	}
	destination := d.destination[:needed]
	written, consumed, err := d.decoder.Transform(destination, d.source, atEOF)
	if errors.Is(err, transform.ErrShortSrc) && !atEOF {
		d.pending = append(d.pending, d.source[consumed:]...)
		err = nil
	}
	return destination[:written], err
}

var _ BinaryReader = (*osBinaryReader)(nil)
