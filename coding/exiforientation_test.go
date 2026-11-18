package coding

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// buildJPEGWithOrientation builds a minimal JPEG carrying an EXIF orientation
// tag, so the parser can be tested without a real photo.
func buildJPEGWithOrientation(orientation int) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00,
		0x08, 0x00, 0x00, 0x00, // IFD offset
		0x01, 0x00, // entry count
		0x12, 0x01, // tag 0x0112 (orientation)
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		byte(orientation), 0x00, 0x00, 0x00, // value
		0x00, 0x00, 0x00, 0x00, // next IFD
	}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	length := len(exif) + 2
	segment := []byte{0xFF, 0xE1, byte(length >> 8), byte(length & 0xFF)}
	out := []byte{0xFF, 0xD8}
	out = append(out, segment...)
	out = append(out, exif...)
	return append(out, 0xFF, 0xD9)
}

// TestExifOrientation covers the JPEG/WebP EXIF parsing and its fallbacks.
func TestExifOrientation(t *testing.T) {
	for _, orientation := range []int{1, 3, 6, 8} {
		if got := ExifOrientation(buildJPEGWithOrientation(orientation)); got != orientation {
			t.Fatalf("orientation %d parsed as %d", orientation, got)
		}
	}
	if got := ExifOrientation([]byte{0xFF, 0xD8, 0xFF, 0xD9}); got != 1 {
		t.Fatalf("JPEG without EXIF = %d", got)
	}
	if got := ExifOrientation([]byte("not an image")); got != 1 {
		t.Fatalf("non-image = %d", got)
	}
	if got := ExifOrientation(nil); got != 1 {
		t.Fatalf("nil = %d", got)
	}
	// A truncated EXIF segment must not panic.
	truncated := buildJPEGWithOrientation(6)
	if got := ExifOrientation(truncated[:len(truncated)-4]); got < 1 || got > 8 {
		t.Fatalf("truncated = %d", got)
	}
}

// TestApplyExifOrientation covers the transform math for a 2x1 image.
func TestApplyExifOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{B: 255, A: 255})

	// Orientation 6 rotates 90 CW into a 1x2: red on top, blue below.
	rotated := ApplyExifOrientation(img, 6)
	if rotated.Bounds().Dx() != 1 || rotated.Bounds().Dy() != 2 {
		t.Fatalf("bounds = %v", rotated.Bounds())
	}
	topR, _, _, _ := rotated.At(0, 0).RGBA()
	_, _, bottomB, _ := rotated.At(0, 1).RGBA()
	if topR>>8 != 255 || bottomB>>8 != 255 {
		t.Fatalf("orientation 6 mapping wrong: top=%v bottom=%v", rotated.At(0, 0), rotated.At(0, 1))
	}

	// Orientation 3 is a 180-degree rotation.
	flipped := ApplyExifOrientation(img, 3)
	fr, _, _, _ := flipped.At(1, 0).RGBA()
	if fr>>8 != 255 {
		t.Fatalf("orientation 3 mapping wrong: %v", flipped.At(1, 0))
	}

	// Orientations 5 and 7 transpose the axes as well (5 is the transposed
	// view, 7 the transverse one), so a 2x1 becomes 1x2.
	if transposed := ApplyExifOrientation(img, 5); transposed.Bounds().Dx() != 1 || transposed.Bounds().Dy() != 2 {
		t.Fatalf("orientation 5 bounds = %v, want 1x2", transposed.Bounds())
	}
	if transverse := ApplyExifOrientation(img, 7); transverse.Bounds().Dx() != 1 || transverse.Bounds().Dy() != 2 {
		t.Fatalf("orientation 7 bounds = %v, want 1x2", transverse.Bounds())
	}

	// Orientation 1 and out-of-range values are unchanged.
	if ApplyExifOrientation(img, 1) != image.Image(img) || ApplyExifOrientation(img, 99) != image.Image(img) {
		t.Fatal("orientation 1/unknown must return the image unchanged")
	}
}

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
