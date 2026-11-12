package ai

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

// FuzzAWSEventStreamMessage feeds Bedrock's binary event stream through the
// reader and, separately, its header block through the header parser. A
// provider is untrusted input; the header block is where lengths index into
// attacker-controlled bytes.
func FuzzAWSEventStreamMessage(f *testing.F) {
	// A well-formed message: prelude(12) + headers + payload + CRC(4).
	build := func(headers, payload []byte) []byte {
		total := 12 + len(headers) + len(payload) + 4
		message := make([]byte, 0, total)
		prelude := make([]byte, 12)
		binary.BigEndian.PutUint32(prelude[0:4], uint32(total))
		binary.BigEndian.PutUint32(prelude[4:8], uint32(len(headers)))
		binary.BigEndian.PutUint32(prelude[8:12], crc32.ChecksumIEEE(prelude[:8]))
		message = append(message, prelude...)
		message = append(message, headers...)
		message = append(message, payload...)
		sum := crc32.ChecksumIEEE(message)
		return binary.BigEndian.AppendUint32(message, sum)
	}
	f.Add([]byte(nil), []byte(nil))
	f.Add([]byte{0x00}, []byte("{}"))
	f.Add([]byte("\x0d:event-type\x07message"), []byte(`{"x":1}`))
	f.Add([]byte{}, []byte("\x00"))
	f.Fuzz(func(t *testing.T, headers, payload []byte) {
		_, _ = parseAWSEventStreamHeaders(headers)
		message := build(headers, payload)
		if _, err := ReadAWSEventStreamMessage(bytes.NewReader(message)); err != nil {
			// A rejected message is fine; a panic is not.
			return
		}
	})
}
