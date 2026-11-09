package coding

import (
	"testing"
	"time"
)

// D415: utils/exif-orientation.ts reads a WebP chunk length as a signed 32-bit
// int (`bytes[o+4] | bytes[o+5]<<8 | bytes[o+6]<<16 | bytes[o+7]<<24`) and then
// advances with `offset = dataStart + chunkSize + (chunkSize % 2)`.
//
// A chunk length with the high bit set therefore goes negative:
//
//   - an EXIF chunk of 0x80000000 passes `dataStart + chunkSize > bytes.length`
//     and `chunkSize >= 6`, so the reference ignores the size and returns the
//     chunk's data as TIFF, reporting its orientation;
//   - a non-EXIF chunk of 0xFFFFFFF8 sets `offset` back to its own start, so the
//     reference's `while (offset + 8 <= bytes.length)` never advances and the
//     call hangs (measured: still looping after 3 s, SIGKILL needed).
//
// Both come from reading a length that cannot be that large. The port reads it
// unsigned and bounds-checks the chunk, so a malformed WebP reports orientation
// 1 and returns promptly. Keeping the port's answer is a deliberate divergence:
// an unreadable orientation leaves the image unrotated, while the reference's
// signed arithmetic can hang the process on a truncated file.
func TestExifWebPChunkSizeWithHighBitSet(t *testing.T) {
	// Minimal little-endian TIFF whose single entry says orientation 6.
	tiff := func() []byte {
		return []byte{
			0x49, 0x49, 0x2a, 0x00, // little-endian TIFF header
			0x08, 0x00, 0x00, 0x00, // first IFD at offset 8
			0x01, 0x00, // one entry
			0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, // no next IFD
		}
	}
	webp := func(chunkID string, size uint32, payload []byte) []byte {
		out := []byte("RIFF")
		body := append([]byte("WEBP"), []byte(chunkID)...)
		body = append(body, byte(size), byte(size>>8), byte(size>>16), byte(size>>24))
		body = append(body, payload...)
		size32 := uint32(len(body))
		out = append(out, byte(size32), byte(size32>>8), byte(size32>>16), byte(size32>>24))
		return append(out, body...)
	}

	// An EXIF chunk claiming 0x80000000 bytes: the reference returns 6.
	if got := ExifOrientation(webp("EXIF", 0x80000000, tiff())); got != 1 {
		t.Errorf("negative EXIF chunk size: orientation %d, want 1", got)
	}

	// A non-EXIF chunk of 0xFFFFFFF8 makes the reference loop forever; the port
	// must return, and it must do so without scanning the file repeatedly.
	started := time.Now()
	if got := ExifOrientation(webp("VP8 ", 0xFFFFFFF8, []byte{1, 2, 3, 4})); got != 1 {
		t.Errorf("negative non-EXIF chunk size: orientation %d, want 1", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("negative chunk size took %v", elapsed)
	}

	// Controls: the same layouts with a size that fits behave as the reference.
	if got := ExifOrientation(webp("EXIF", uint32(len(tiff())), tiff())); got != 6 {
		t.Errorf("well-formed EXIF chunk: orientation %d, want 6", got)
	}
	if got := ExifOrientation(webp("VP8 ", 4, []byte{1, 2, 3, 4})); got != 1 {
		t.Errorf("well-formed non-EXIF chunk: orientation %d, want 1", got)
	}
}
