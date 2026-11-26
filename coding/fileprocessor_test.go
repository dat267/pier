package coding

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for utils/mime.ts, utils/image-resize-core.ts, and
// cli/file-processor.ts behavior.

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestDetectSupportedImageMimeType(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}
	if got := DetectSupportedImageMimeType(jpeg); got != "image/jpeg" {
		t.Fatalf("jpeg = %q", got)
	}
	// 0xf7 marks JPEG 2000, which is rejected.
	if got := DetectSupportedImageMimeType([]byte{0xff, 0xd8, 0xff, 0xf7}); got != "" {
		t.Fatalf("jpeg2000 = %q", got)
	}
	if got := DetectSupportedImageMimeType(pngBytes(t, 2, 2)); got != "image/png" {
		t.Fatalf("png = %q", got)
	}
	// A PNG signature without an IHDR chunk is rejected.
	broken := append(append([]byte{}, pngSignature...), 0, 0, 0, 1, 'X', 'X', 'X', 'X')
	if got := DetectSupportedImageMimeType(broken); got != "" {
		t.Fatalf("broken png = %q", got)
	}
	// An animation control chunk before IDAT marks an animated PNG: walk the
	// chunks (length, type, data, CRC).
	animated := append([]byte{}, pngSignature...)
	animated = append(animated, 0, 0, 0, 13)
	animated = append(animated, []byte("IHDR")...)
	animated = append(animated, make([]byte, 13+4)...) // data + CRC
	animated = append(animated, 0, 0, 0, 0)
	animated = append(animated, []byte("acTL")...)
	animated = append(animated, 0, 0, 0, 0)
	if got := DetectSupportedImageMimeType(animated); got != "" {
		t.Fatalf("animated png = %q", got)
	}
	// A static PNG (IDAT before any acTL) is accepted.
	staticPNG := append([]byte{}, pngSignature...)
	staticPNG = append(staticPNG, 0, 0, 0, 13)
	staticPNG = append(staticPNG, []byte("IHDR")...)
	staticPNG = append(staticPNG, make([]byte, 13+4)...)
	staticPNG = append(staticPNG, 0, 0, 0, 0)
	staticPNG = append(staticPNG, []byte("IDAT")...)
	staticPNG = append(staticPNG, 0, 0, 0, 0)
	if got := DetectSupportedImageMimeType(staticPNG); got != "image/png" {
		t.Fatalf("static png = %q", got)
	}
	if got := DetectSupportedImageMimeType([]byte("GIF89a")); got != "image/gif" {
		t.Fatalf("gif = %q", got)
	}
	webp := append([]byte("RIFF"), 0, 0, 0, 0)
	webp = append(webp, []byte("WEBP")...)
	if got := DetectSupportedImageMimeType(webp); got != "image/webp" {
		t.Fatalf("webp = %q", got)
	}
	// A BMP needs a valid header structure.
	if got := DetectSupportedImageMimeType([]byte("BM")); got != "" {
		t.Fatalf("short bmp = %q", got)
	}
	bmp := make([]byte, 30)
	copy(bmp, "BM")
	bmp[2] = 70  // declared file size (must exceed the pixel data offset)
	bmp[10] = 54 // pixel data offset (14 + 40 header bytes)
	bmp[14] = 40 // DIB header size
	bmp[26] = 1  // color planes
	bmp[28] = 24 // bits per pixel
	if got := DetectSupportedImageMimeType(bmp); got != "image/bmp" {
		t.Fatalf("bmp = %q", got)
	}
	bmp[28] = 7 // unsupported bit depth
	if got := DetectSupportedImageMimeType(bmp); got != "" {
		t.Fatalf("bad bmp = %q", got)
	}
	if got := DetectSupportedImageMimeType([]byte("plain text")); got != "" {
		t.Fatalf("text = %q", got)
	}
}

func TestDetectSupportedImageMimeTypeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, pngBytes(t, 3, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	mimeType, err := DetectSupportedImageMimeTypeFromFile(path)
	if err != nil || mimeType != "image/png" {
		t.Fatalf("mime = %q err = %v", mimeType, err)
	}
	if _, err := DetectSupportedImageMimeTypeFromFile(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("missing files must error")
	}
}

func TestProcessImagePassesThroughSmallImages(t *testing.T) {
	data := pngBytes(t, 4, 4)
	result := ProcessImage(data, "image/png", nil)
	if !result.OK {
		t.Fatalf("result = %+v", result)
	}
	if result.MimeType != "image/png" {
		t.Fatalf("mime = %s", result.MimeType)
	}
	if result.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatal("an image within the limits must pass through byte-identically")
	}
	if len(result.Hints) != 0 {
		t.Fatalf("hints = %#v", result.Hints)
	}

	// image/jpg normalizes to image/jpeg.
	result = ProcessImage(data, "image/jpg", nil)
	if !result.OK || result.MimeType != "image/jpeg" {
		t.Fatalf("result = %+v", result)
	}

	// Without auto-resize the original bytes are returned with a conversion hint.
	result = ProcessImage(data, "image/jpg", &ProcessImageOptions{AutoResizeImages: new(false)})
	if !result.OK || len(result.Hints) != 0 {
		t.Fatalf("result = %+v", result)
	}

	// An unsupported format reports the upstream omission message: the Go port
	// cannot convert it without Photon.
	result = ProcessImage([]byte("not an image"), "application/octet-stream", nil)
	if result.OK || result.Message != "[Image omitted: could not be converted to a supported inline image format.]" {
		t.Fatalf("result = %+v", result)
	}

	// An image that needs resizing is scaled to the max dimensions and carries the
	// dimension note. This used to be an omission, because the port had no
	// resizer.
	large := pngBytes(t, 2100, 4)
	result = ProcessImage(large, "image/png", nil)
	if !result.OK {
		t.Fatalf("an oversized image should be resized, got %+v", result)
	}
	if result.MimeType != "image/png" {
		t.Fatalf("mime = %s", result.MimeType)
	}
	resizedBytes, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatalf("data is not base64: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(resizedBytes))
	if err != nil {
		t.Fatalf("resized bytes do not decode: %v", err)
	}
	if config.Width != 2000 || config.Height != 4 {
		t.Fatalf("resized to %dx%d, want 2000x4", config.Width, config.Height)
	}
	if len(result.Hints) != 1 {
		t.Fatalf("hints = %#v, want the dimension note", result.Hints)
	}

	// A corrupt supported-format image cannot be measured and is omitted.
	result = ProcessImage([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00}, "image/jpeg", nil)
	if result.OK || result.Message != "[Image omitted: could not be resized below the inline image size limit.]" {
		t.Fatalf("result = %+v", result)
	}
}

// file-processor.ts reads text files with UTF-8 replacement semantics.
func TestProcessFileArgumentsReplacesMalformedUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(path, []byte{0xff, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	processed, err := ProcessFileArguments([]string{path}, &ProcessFileOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	want := "<file name=\"" + path + "\">\n\uFFFDx\n</file>\n"
	if processed.Text != want {
		t.Fatalf("processed text = %q; want %q", processed.Text, want)
	}
}

func TestProcessFileArgumentsTextAndImages(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("\ufeffhello\nworld"), 0o644); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, pngBytes(t, 5, 5), 0o644); err != nil {
		t.Fatal(err)
	}

	processed, err := ProcessFileArguments([]string{textPath, emptyPath, imagePath}, &ProcessFileOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	// The text file contributes its BOM-stripped content; the empty file is
	// skipped; the image adds a reference and an attachment.
	wantText := "<file name=\"" + textPath + "\">\nhello\nworld\n</file>\n" +
		"<file name=\"" + imagePath + "\"></file>\n"
	if processed.Text != wantText {
		t.Fatalf("text = %q, want %q", processed.Text, wantText)
	}
	if len(processed.Images) != 1 || processed.Images[0].MimeType != "image/png" || processed.Images[0].Data == "" {
		t.Fatalf("images = %+v", processed.Images)
	}

	// A missing file is an error naming the resolved path.
	if _, err := ProcessFileArguments([]string{filepath.Join(dir, "missing.txt")}, &ProcessFileOptions{Cwd: dir}); err == nil ||
		!strings.Contains(err.Error(), "File not found: ") {
		t.Fatalf("err = %v", err)
	}

	// Relative paths resolve against the cwd option.
	if err := os.WriteFile(filepath.Join(dir, "relative.txt"), []byte("relative"), 0o644); err != nil {
		t.Fatal(err)
	}
	processed, err = ProcessFileArguments([]string{"relative.txt"}, &ProcessFileOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(processed.Text, "relative") || !strings.Contains(processed.Text, filepath.Join(dir, "relative.txt")) {
		t.Fatalf("text = %q", processed.Text)
	}

	// No file arguments produce empty output.
	processed, err = ProcessFileArguments(nil, &ProcessFileOptions{Cwd: dir})
	if err != nil || processed.Text != "" || len(processed.Images) != 0 {
		t.Fatalf("processed = %+v err = %v", processed, err)
	}
}

// mime.ts requires the GIF87a/GIF89a version stamp; the port matched any "GIF".
func TestDetectSupportedImageMimeTypeRejectsUnstampedGIF(t *testing.T) {
	if got := DetectSupportedImageMimeType([]byte("GIFplain")); got != "" {
		t.Fatalf("unstamped GIF = %q; want empty", got)
	}
	if got := DetectSupportedImageMimeType([]byte("GIF87a")); got != "image/gif" {
		t.Fatalf("GIF87a = %q; want image/gif", got)
	}
}

// A sliver image whose scaled height rounds to zero must not reach an encoder.
// inlineImageTargetSize keeps the aspect ratio with math.Round, so a 5000x1
// image that fits MaxWidth targets 2000x0; every Go image encoder rejects a
// zero dimension. resizeInlineImage drops such an image instead.
func TestProcessImageDropsASliverThatRoundsToZero(t *testing.T) {
	// Tall enough to need scaling, thin enough that the scaled side rounds to 0.
	data := pngBytes(t, 5000, 1)
	result := ProcessImage(data, "image/png", nil)
	if result.OK {
		t.Fatalf("a 2000x0 target was encoded as %+v", result)
	}
	if !strings.Contains(result.Message, "Could not process image") && result.Message == "" {
		t.Fatalf("message = %q, want a reported omission", result.Message)
	}
}

// A BMP has no pass-through path: normalizeImage refuses it BEFORE the
// autoResizeImages branch, exactly as the reference does (image-process.ts:78-84
// normalises first), so the omission message is the same with resizing on or off.
// The comment used to claim auto-resize off let a bmp through unchanged, which
// no code path does. D408 is the divergence that makes this visible: Photon
// converts bmp upstream and the Go port cannot.
func TestProcessImageOmitsBMPWithResizeOnOrOff(t *testing.T) {
	const dib = 40
	rowSize := ((2*3 + 3) / 4) * 4
	size := 14 + dib + rowSize*2
	bmp := make([]byte, size)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:], uint32(size))
	binary.LittleEndian.PutUint32(bmp[10:], 14+dib)
	binary.LittleEndian.PutUint32(bmp[14:], dib)
	binary.LittleEndian.PutUint32(bmp[18:], 2)
	binary.LittleEndian.PutUint32(bmp[22:], 2)
	binary.LittleEndian.PutUint16(bmp[26:], 1)
	binary.LittleEndian.PutUint16(bmp[28:], 24)

	// The sniffer names the format even though the converter cannot inline it.
	if got := DetectSupportedImageMimeType(bmp); got != "image/bmp" {
		t.Fatalf("sniff = %q, want image/bmp", got)
	}
	const omission = "[Image omitted: could not be converted to a supported inline image format.]"
	for _, autoResize := range []bool{true, false} {
		value := autoResize
		result := ProcessImage(bmp, "image/bmp", &ProcessImageOptions{AutoResizeImages: &value})
		if result.OK || result.Message != omission {
			t.Fatalf("autoResize=%v: ok=%v message=%q, want the omission message", autoResize, result.OK, result.Message)
		}
	}
}
