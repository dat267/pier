package ai

import (
	"bufio"
	"bytes"
)

// newSSELineSplitter recognizes the CR, LF and CRLF boundaries accepted by the
// reference's SSE decoders. A CR completes a line immediately; a following LF
// is consumed as its continuation even when it arrives in the next read.
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
