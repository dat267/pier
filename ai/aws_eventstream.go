package ai

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// The AWS event-stream binary protocol used by Bedrock ConverseStream. Upstream
// hands the response to the AWS SDK's async iterator; the Go port decodes the
// frames directly (12-byte prelude, headers, payload, 4-byte message CRC).

const (
	awsEventStreamPreludeLength = 12
	// awsEventStreamReadChunk is how much of a frame's body is allocated per read,
	// so a truncated frame costs what it actually delivered.
	awsEventStreamReadChunk     = 64 << 10
	awsEventStreamMinMessageLen = 16

	// Header value type ids.
	awsHeaderBoolTrue  = 0
	awsHeaderBoolFalse = 1
	awsHeaderByte      = 2
	awsHeaderShort     = 3
	awsHeaderInt       = 4
	awsHeaderLong      = 5
	awsHeaderByteArray = 6
	awsHeaderString    = 7
	awsHeaderTimestamp = 8
	awsHeaderUUID      = 9
)

// AWSEventStreamMessage is one decoded event-stream message.
type AWSEventStreamMessage struct {
	Headers map[string]any
	Payload []byte
}

// AWSEventStreamError reports a malformed frame.
type AWSEventStreamError struct {
	Message string
}

func (e *AWSEventStreamError) Error() string { return e.Message }

// ReadAWSEventStreamMessage reads one message, returning io.EOF at a clean end.
func ReadAWSEventStreamMessage(reader io.Reader) (*AWSEventStreamMessage, error) {
	prelude := make([]byte, awsEventStreamPreludeLength)
	if _, err := io.ReadFull(reader, prelude); err != nil {
		return nil, err
	}
	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	preludeCRC := binary.BigEndian.Uint32(prelude[8:12])
	if actual := crc32.ChecksumIEEE(prelude[:8]); actual != preludeCRC {
		return nil, &AWSEventStreamError{Message: "AWS event stream prelude checksum mismatch"}
	}
	if totalLength < awsEventStreamMinMessageLen || totalLength > 16<<20 {
		return nil, &AWSEventStreamError{Message: fmt.Sprintf("AWS event stream message length %d is invalid", totalLength)}
	}
	if headersLength > totalLength-awsEventStreamMinMessageLen {
		return nil, &AWSEventStreamError{Message: "AWS event stream headers length is invalid"}
	}

	// The announced length comes from the peer, so grow the buffer as bytes arrive
	// rather than allocating the full maximum up front: a twelve-byte prelude
	// claiming 16 MiB must not cost 16 MiB before the body does.
	remaining := int(totalLength - awsEventStreamPreludeLength)
	rest := make([]byte, 0, min(remaining, awsEventStreamReadChunk))
	for remaining > 0 {
		chunk := min(remaining, awsEventStreamReadChunk)
		at := len(rest)
		rest = append(rest, make([]byte, chunk)...)
		n, err := io.ReadFull(reader, rest[at:])
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		rest = rest[:at+n]
		remaining -= n
	}
	messageCRC := binary.BigEndian.Uint32(rest[len(rest)-4:])
	if actual := crc32.ChecksumIEEE(append(append([]byte{}, prelude...), rest[:len(rest)-4]...)); actual != messageCRC {
		return nil, &AWSEventStreamError{Message: "AWS event stream message checksum mismatch"}
	}

	headers, err := parseAWSEventStreamHeaders(rest[:headersLength])
	if err != nil {
		return nil, err
	}
	payload := rest[headersLength : len(rest)-4]
	return &AWSEventStreamMessage{Headers: headers, Payload: payload}, nil
}

// parseAWSEventStreamHeaders decodes the header block.
func parseAWSEventStreamHeaders(raw []byte) (map[string]any, error) {
	headers := map[string]any{}
	offset := 0
	for offset < len(raw) {
		// The header name length is one byte and a string or byte-array value
		// length is two, big-endian (AWS event-stream spec; the AWS SDK's
		// HeaderMarshaller.parse reads getUint8 and getUint16).
		nameLength := int(raw[offset])
		offset++
		if offset+nameLength > len(raw) {
			return nil, &AWSEventStreamError{Message: "AWS event stream header name is truncated"}
		}
		name := string(raw[offset : offset+nameLength])
		offset += nameLength
		if offset >= len(raw) {
			return nil, &AWSEventStreamError{Message: "AWS event stream header type is truncated"}
		}
		valueType := raw[offset]
		offset++
		value, next, err := parseAWSEventStreamHeaderValue(raw, offset, valueType)
		if err != nil {
			return nil, err
		}
		offset = next
		headers[name] = value
	}
	return headers, nil
}

func parseAWSEventStreamHeaderValue(raw []byte, offset int, valueType byte) (any, int, error) {
	need := func(count int) error {
		if offset+count > len(raw) {
			return &AWSEventStreamError{Message: "AWS event stream header value is truncated"}
		}
		return nil
	}
	switch valueType {
	case awsHeaderBoolTrue:
		return true, offset, nil
	case awsHeaderBoolFalse:
		return false, offset, nil
	case awsHeaderByte:
		if err := need(1); err != nil {
			return nil, 0, err
		}
		return int8(raw[offset]), offset + 1, nil
	case awsHeaderShort:
		if err := need(2); err != nil {
			return nil, 0, err
		}
		return int16(binary.BigEndian.Uint16(raw[offset:])), offset + 2, nil
	case awsHeaderInt:
		if err := need(4); err != nil {
			return nil, 0, err
		}
		return int32(binary.BigEndian.Uint32(raw[offset:])), offset + 4, nil
	case awsHeaderLong, awsHeaderTimestamp:
		if err := need(8); err != nil {
			return nil, 0, err
		}
		return int64(binary.BigEndian.Uint64(raw[offset:])), offset + 8, nil
	case awsHeaderByteArray, awsHeaderString:
		if err := need(2); err != nil {
			return nil, 0, err
		}
		length := int(binary.BigEndian.Uint16(raw[offset:]))
		offset += 2
		if offset+length > len(raw) {
			return nil, 0, &AWSEventStreamError{Message: "AWS event stream header value is truncated"}
		}
		bytes := raw[offset : offset+length]
		if valueType == awsHeaderString {
			return string(bytes), offset + length, nil
		}
		return append([]byte{}, bytes...), offset + length, nil
	case awsHeaderUUID:
		if err := need(16); err != nil {
			return nil, 0, err
		}
		return raw[offset : offset+16], offset + 16, nil
	default:
		return nil, 0, &AWSEventStreamError{Message: fmt.Sprintf("AWS event stream header type %d is unknown", valueType)}
	}
}
