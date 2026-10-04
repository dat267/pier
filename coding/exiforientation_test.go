package coding

import (
	"image"
	"image/color"
	"testing"
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

	// Orientation 1 and out-of-range values are unchanged.
	if ApplyExifOrientation(img, 1) != image.Image(img) || ApplyExifOrientation(img, 99) != image.Image(img) {
		t.Fatal("orientation 1/unknown must return the image unchanged")
	}
}
