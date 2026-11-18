package coding

import (
	"bytes"
	"encoding/base64"
	"image"
	"strings"
	"testing"
)

// tinyWebP is a 2x2 lossless WebP (38 bytes), generated once with
// `magick -size 2x2 xc:'#336699' -define webp:lossless=true`.
func tinyWebP(t *testing.T) []byte {
	t.Helper()
	data := readFileBytes(t, "testdata/images/tiny.webp")
	return data
}

// pi v1.5.2 (b4afdddf) packages/coding-agent/src/utils/image-process.ts
// normalizeImage keeps image/webp as a supported inline type and
// resizeImageInProcess decodes it with Photon, so an image within every limit is
// returned byte-identically. Go's standard library has no WebP decoder, so the
// port used to take the decode-failure path and omit the image.
func TestProcessImageEmbedsASmallWebP(t *testing.T) {
	data := tinyWebP(t)
	result := ProcessImage(data, "image/webp", nil)
	if !result.OK {
		t.Fatalf("a 2x2 WebP was not embedded: %q", result.Message)
	}
	if result.MimeType != "image/webp" {
		t.Errorf("MimeType = %q, want image/webp (the bytes pass through)", result.MimeType)
	}
	if result.Data != base64.StdEncoding.EncodeToString(data) {
		t.Error("the bytes were re-encoded; an image within every limit passes through")
	}
	if len(result.Hints) != 0 {
		t.Errorf("Hints = %v, want none for an unmodified image", result.Hints)
	}
}

// The decoded pixels are what a resize needs: a WebP above MaxWidth is scaled and
// re-encoded like any other inline image.
func TestProcessImageResizesALargeWebP(t *testing.T) {
	options := &ProcessImageOptions{ResizeOptions: &ImageResizeOptions{
		MaxWidth:    1,
		MaxHeight:   1,
		MaxBytes:    DefaultImageResizeOptions.MaxBytes,
		JPEGQuality: DefaultImageResizeOptions.JPEGQuality,
	}}
	result := ProcessImage(tinyWebP(t), "image/webp", options)
	if !result.OK {
		t.Fatalf("a 2x2 WebP capped at 1x1 was not embedded: %q", result.Message)
	}
	if result.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png after a resize", result.MimeType)
	}
	if len(result.Hints) != 1 || !strings.Contains(result.Hints[0], "original 2x2, displayed at 1x1") {
		t.Errorf("Hints = %v, want the dimension note for 2x2 to 1x1", result.Hints)
	}
	decoded, ok := DecodeImageBase64(result.Data)
	if !ok {
		t.Fatal("the resized payload is not base64")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("the resized payload does not decode: %v", err)
	}
	if config.Width != 1 || config.Height != 1 {
		t.Errorf("resized to %dx%d, want 1x1", config.Width, config.Height)
	}
}

// The kitty graphics protocol only accepts PNG, so a WebP shown in a kitty
// terminal goes through EncodeImageAsPNG (upstream ensurePngTranscoder, #10292).
func TestEncodeImageAsPNGTranscodesWebP(t *testing.T) {
	encoded, ok := EncodeImageAsPNG(base64.StdEncoding.EncodeToString(tinyWebP(t)), "image/webp")
	if !ok {
		t.Fatal("a valid WebP was not transcoded")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("the transcoded payload does not decode: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}
	if config.Width != 2 || config.Height != 2 {
		t.Errorf("transcoded to %dx%d, want 2x2", config.Width, config.Height)
	}
}
