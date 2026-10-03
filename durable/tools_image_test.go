package durable

import "testing"

// Port of tools/image.ts.

func pngBuffer(animated bool) []byte {
	buffer := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	buffer = append(buffer, 0, 0, 0, 13)
	buffer = append(buffer, []byte("IHDR")...)
	buffer = append(buffer, make([]byte, 13)...)
	buffer = append(buffer, 0, 0, 0, 0) // CRC
	if animated {
		buffer = append(buffer, 0, 0, 0, 0)
		buffer = append(buffer, []byte("acTL")...)
	}
	return buffer
}

func bmpBuffer() []byte {
	buffer := make([]byte, 60)
	buffer[0], buffer[1] = 'B', 'M'
	putUint32LE(buffer, 2, 100) // declared file size
	putUint32LE(buffer, 10, 54) // pixel data offset
	putUint32LE(buffer, 14, 40) // DIB header size
	putUint16LE(buffer, 26, 1)  // color planes
	putUint16LE(buffer, 28, 24) // bits per pixel
	return buffer
}

func putUint16LE(buffer []byte, offset int, value int) {
	buffer[offset] = byte(value)
	buffer[offset+1] = byte(value >> 8)
}

func putUint32LE(buffer []byte, offset int, value int) {
	buffer[offset] = byte(value)
	buffer[offset+1] = byte(value >> 8)
	buffer[offset+2] = byte(value >> 16)
	buffer[offset+3] = byte(value >> 24)
}

func TestDetectSupportedImageMimeType(t *testing.T) {
	cases := []struct {
		name   string
		buffer []byte
		want   string
	}{
		{"png", pngBuffer(false), "image/png"},
		{"animated png", pngBuffer(true), ""},
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00}, "image/jpeg"},
		{"jpeg reserved marker", []byte{0xff, 0xd8, 0xff, 0xf7}, ""},
		{"gif87", append([]byte("GIF87a"), make([]byte, 10)...), "image/gif"},
		{"gif89", append([]byte("GIF89a"), make([]byte, 10)...), "image/gif"},
		{"webp", append(append([]byte("RIFF"), 0, 0, 0, 0), []byte("WEBP")...), "image/webp"},
		{"bmp", bmpBuffer(), "image/bmp"},
		{"garbage", []byte("not an image"), ""},
		{"empty", nil, ""},
	}
	for _, testCase := range cases {
		if got := DetectSupportedImageMimeType(testCase.buffer); got != testCase.want {
			t.Fatalf("%s = %q, want %q", testCase.name, got, testCase.want)
		}
	}
	// A BMP with an implausible pixel offset is rejected.
	broken := bmpBuffer()
	putUint32LE(broken, 10, 10)
	if got := DetectSupportedImageMimeType(broken); got != "" {
		t.Fatalf("broken bmp = %q", got)
	}
}
