package coding

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), utils/mime.ts detectSupportedImageMimeType.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/image-mime/generate.mjs ./pi > coding/testdata/image-mime/golden.json
func TestDetectSupportedImageMimeTypeMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/image-mime/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string  `json:"name"`
		Hex      string  `json:"hex"`
		Expected *string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty image-mime corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			buffer, err := hex.DecodeString(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			got := DetectSupportedImageMimeType(buffer)
			want := ""
			if fixture.Expected != nil {
				want = *fixture.Expected
			}
			if got != want {
				t.Fatalf("mime = %q, want %q", got, want)
			}
		})
	}
}
