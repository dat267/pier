package coding

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestDecodeImageBase64MatchesNode pins the lenient decoder the image paths use
// against Node's Buffer.from(text, "base64"), which upstream relies on
// (utils/tool-result-images.ts normalizeToolResultImages and
// core/agent-session.ts _normalizePromptImages both call
// Buffer.from(image.data, "base64")). Node ignores characters outside the
// alphabet, accepts padding or none, and takes the URL-safe alphabet too.
func TestDecodeImageBase64MatchesNode(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string // hex
	}{
		{"padded", "aGVsbG8=", "68656c6c6f"},
		{"unpadded", "aGVsbG8", "68656c6c6f"},
		{"urlsafe", "a-_b", "6befdb"},
		{"standard", "a+b/", "6be6ff"},
		{"wrapped", "aGVs\nbG8=", "68656c6c6f"},
		{"spaces", "aGVs bG8=", "68656c6c6f"},
		{"tabs", "aGVs\tbG8=", "68656c6c6f"},
		{"stops-at-padding", "aG=sbG8", "68"},
		{"single-char-dropped", "a", ""},
		{"empty", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data, ok := DecodeImageBase64(test.data)
			if !ok {
				t.Fatalf("%q was rejected", test.data)
			}
			if got := fmtHex(data); got != test.want {
				t.Errorf("decoded %q to %s, want %s", test.data, got, test.want)
			}
		})
	}
}

func fmtHex(data []byte) string {
	const digits = "0123456789abcdef"
	var builder strings.Builder
	for _, b := range data {
		builder.WriteByte(digits[b>>4])
		builder.WriteByte(digits[b&0x0f])
	}
	return builder.String()
}

// TestToolResultImagesNormalizeUnpaddedAndURLSafeBase64 pins that an image
// block reaches the same size limits whatever base64 alphabet carried it.
// Upstream decodes with Buffer.from(data, "base64"), so an unpadded payload is
// the same image and goes through processImage, which resizes it under the
// 4.5MB inline cap.
func TestToolResultImagesNormalizeUnpaddedAndURLSafeBase64(t *testing.T) {
	raw := noisyPNG(t, 300, 300)
	standard := base64.StdEncoding.EncodeToString(raw)
	unpadded := strings.TrimRight(standard, "=")
	urlsafe := strings.NewReplacer("+", "-", "/", "_").Replace(unpadded)
	for _, test := range []struct{ name, data string }{
		{"standard", standard},
		{"unpadded", unpadded},
		{"urlsafe", urlsafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := ai.UserContentList{ai.ImageContent{Data: test.data, MimeType: "image/png"}}
			normalized, changed := NormalizeToolResultImages(content, nil)
			if len(normalized) != 1 {
				t.Fatalf("normalized = %d blocks", len(normalized))
			}
			block, ok := normalized[0].(ai.ImageContent)
			if !ok {
				t.Fatalf("normalized block = %T", normalized[0])
			}
			decoded, err := base64.StdEncoding.DecodeString(block.Data)
			if err != nil {
				t.Fatalf("normalized data is not standard base64: %v", err)
			}
			if len(decoded) != len(raw) {
				t.Errorf("%s: normalized image is %d bytes, want the original %d (changed=%v)",
					test.name, len(decoded), len(raw), changed)
			}
		})
	}
}
