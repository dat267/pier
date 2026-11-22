package ai

import (
	"bufio"
	"bytes"
	"io"
)

// newSSELineSplitter recognizes the CR, LF and CRLF boundaries accepted by the
// reference's SSE decoders. A CR completes a line immediately; a following LF
// is consumed as its continuation even when it arrives in the next read.
func newSSEScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Split(newSSELineSplitter())
	scanner.Buffer(make([]byte, 0, 64*1024), sseMaxLineBytes)
	return scanner
}

// sseMaxLineBytes bounds one SSE line. The reference has no limit at all
// (openai/src/internal/decoders/line.ts), so its buffer grows with the line: measured
// at 127 MiB allocated for a 64 MiB endless line with no error. A cap keeps the port
// responsive (AGENTS.md, D510) but the cap is not free, because bufio.Scanner doubles
// its buffer: at 256 MiB a single over-long line peaked at 831 MiB, worse than the
// unbounded growth it replaces. 64 MiB is chosen to sit above the widest real event
// (`TestOpenAIChunkParserAcceptsLargeDataLine` pins a 16 MiB data line) while keeping
// the doubling peak near 207 MiB. D510 records the measurement and the rejected cap.
const sseMaxLineBytes = 64 << 20

func newSSELineSplitter() bufio.SplitFunc {
	skipLF := false
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if skipLF {
			if len(data) == 0 {
				return 0, nil, nil
			}
			skipLF = false
			if data[0] == '\n' {
				return 1, nil, nil
			}
		}
		if index := bytes.IndexAny(data, "\r\n"); index >= 0 {
			advance = index + 1
			if data[index] == '\r' {
				if advance < len(data) && data[advance] == '\n' {
					advance++
				} else if advance == len(data) {
					skipLF = true
				}
			}
			return advance, data[:index], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}
