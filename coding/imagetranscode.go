package coding

// PNG transcoding for the inline-image path. The kitty graphics protocol only
// accepts PNG (f=100), so a non-PNG image shown in a kitty terminal must be
// re-encoded first (upstream #10292, ensurePngTranscoder). The decoders
// registered in fileprocessor.go cover every inline type the sniffer accepts
// (PNG, JPEG, GIF, WebP); a payload none of them decodes falls back to the text
// placeholder.

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
	decoded, ok := DecodeImageBase64(base64Data)
	if !ok {
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
