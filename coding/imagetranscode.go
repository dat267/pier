package coding

// PNG transcoding for the inline-image path. The kitty graphics protocol only
// accepts PNG (f=100), so a non-PNG image shown in a kitty terminal must be
// re-encoded first (upstream #10292, ensurePngTranscoder). Go's standard
// decoders cover PNG, JPEG and GIF; formats the port cannot decode (WebP, BMP)
// fall back to the text placeholder (D26).

import (
	"bytes"
	"encoding/base64"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// EncodeImageAsPNG decodes base64 image data and re-encodes it as base64 PNG.
// PNG input is returned unchanged.
func EncodeImageAsPNG(base64Data string, mimeType string) (string, bool) {
	if mimeType == "image/png" {
		return base64Data, true
	}
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", false
	}
	source, err := decodeInlineImage(decoded)
	if err != nil {
		return "", false
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes()), true
}
