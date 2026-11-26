package coding

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestEncodeImageAsPNG(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 4, 3))
	source.Set(1, 1, color.RGBA{R: 255, A: 255})
	var jpegBuffer bytes.Buffer
	if err := jpeg.Encode(&jpegBuffer, source, nil); err != nil {
		t.Fatal(err)
	}
	jpeg64 := base64.StdEncoding.EncodeToString(jpegBuffer.Bytes())

	encoded, ok := EncodeImageAsPNG(jpeg64, "image/jpeg")
	if !ok {
		t.Fatal("jpeg transcode failed")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("result is not PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 3 {
		t.Fatalf("bounds = %v", decoded.Bounds())
	}

	// PNG input passes through unchanged.
	if png64, ok := EncodeImageAsPNG(jpeg64, "image/png"); !ok || png64 != jpeg64 {
		t.Fatalf("png passthrough = %v", ok)
	}
	// An undecodable format fails rather than producing garbage.
	if _, ok := EncodeImageAsPNG(base64.StdEncoding.EncodeToString([]byte("not an image")), "image/webp"); ok {
		t.Fatal("webp transcode should fail")
	}
}

// insertExifOrientation splices an APP1 EXIF segment holding one orientation
// value immediately after the JPEG SOI marker.
func insertExifOrientation(jpegData []byte, orientation byte) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
	tiff = append(tiff, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, orientation, 0, 0, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segmentLength := len(payload) + 2
	segment := []byte{0xFF, 0xE1, byte(segmentLength >> 8), byte(segmentLength)}
	segment = append(segment, payload...)
	out := append([]byte{}, jpegData[:2]...)
	out = append(out, segment...)
	return append(out, jpegData[2:]...)
}

// TestEncodeImageAsPNGAppliesExifOrientation pins upstream's ensurePngTranscoder
// path: utils/image-convert.ts encodePng calls applyExifOrientation before
// get_bytes, so an orientation-6 JPEG is re-encoded rotated. Go's jpeg decoder
// ignores the tag, so without the correction the PNG keeps the unrotated
// dimensions and a rotated photo shows sideways in a kitty terminal.
func TestEncodeImageAsPNGAppliesExifOrientation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			source.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 12), B: 128, A: 255})
		}
	}
	var jpegBuffer bytes.Buffer
	if err := jpeg.Encode(&jpegBuffer, source, nil); err != nil {
		t.Fatal(err)
	}
	data := insertExifOrientation(jpegBuffer.Bytes(), 6)
	if got := ExifOrientation(data); got != 6 {
		t.Fatalf("fixture orientation = %d, want 6", got)
	}
	encoded, ok := EncodeImageAsPNG(base64.StdEncoding.EncodeToString(data), "image/jpeg")
	if !ok {
		t.Fatal("EncodeImageAsPNG returned !ok")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	// Orientation 6 rotates 90 degrees clockwise, swapping the dimensions.
	if config.Width != 20 || config.Height != 40 {
		t.Errorf("encoded PNG is %dx%d, want 20x40 (orientation 6 rotates 40x20)", config.Width, config.Height)
	}
}

// TestEncodeImageAsPNGKeepsUprightOrientation pins the no-op half: orientation 1
// keeps the source dimensions, so the correction is not applied unconditionally.
func TestEncodeImageAsPNGKeepsUprightOrientation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 40, 20))
	var jpegBuffer bytes.Buffer
	if err := jpeg.Encode(&jpegBuffer, source, nil); err != nil {
		t.Fatal(err)
	}
	data := insertExifOrientation(jpegBuffer.Bytes(), 1)
	encoded, ok := EncodeImageAsPNG(base64.StdEncoding.EncodeToString(data), "image/jpeg")
	if !ok {
		t.Fatal("EncodeImageAsPNG returned !ok")
	}
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	config, err := png.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 40 || config.Height != 20 {
		t.Errorf("encoded PNG is %dx%d, want 40x20 for orientation 1", config.Width, config.Height)
	}
}
