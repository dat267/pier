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
