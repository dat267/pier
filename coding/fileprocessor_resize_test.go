package coding

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

// noisyPNG builds a PNG with incompressible content, so the byte budget is what
// the resize loop has to satisfy rather than PNG's own compression.
func noisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	random := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(random.Intn(256))
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func solidPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = 0x7f
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestResizeInlineImageFitsTheMaxDimensions covers the resizer that used to be
// missing: upstream's resizeImageInProcess fits the image to maxWidth/maxHeight
// and re-encodes it, where the port used to return nil (and the caller then
// reported an omission).
func TestResizeInlineImageFitsTheMaxDimensions(t *testing.T) {
	resized := resizeInlineImage(solidPNG(t, 3000, 100), "image/png", DefaultImageResizeOptions)
	if resized == nil {
		t.Fatal("a 3000x100 image was not resized")
	}
	if !resized.WasResized {
		t.Error("WasResized = false for an image that had to be scaled")
	}
	if resized.Width != 2000 || resized.Height != 67 {
		t.Fatalf("resized to %dx%d, want 2000x67 (aspect kept, upstream rounding)", resized.Width, resized.Height)
	}
	if resized.OriginalWidth != 3000 || resized.OriginalHeight != 100 {
		t.Errorf("original reported as %dx%d", resized.OriginalWidth, resized.OriginalHeight)
	}
	data, err := base64.StdEncoding.DecodeString(resized.Data)
	if err != nil {
		t.Fatalf("data is not base64: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("resized bytes do not decode: %v", err)
	}
	if config.Width != resized.Width || config.Height != resized.Height {
		t.Errorf("encoded image is %dx%d, want %dx%d", config.Width, config.Height, resized.Width, resized.Height)
	}
	if note := FormatDimensionNote(resized); note == "" {
		t.Error("a resized image should carry the dimension note hint")
	}
}

// Pinned image-resize-core.ts defaults maxBytes to 4.5*1024*1024. An image
// under that encoded size and within dimensions should pass through unchanged.
func TestProcessImageUsesUpstreamDefaultByteLimit(t *testing.T) {
	data := noisyPNG(t, 930, 930)
	encodedSize := (len(data) + 2) / 3 * 4
	if encodedSize <= 4_500_000 || encodedSize >= 4_718_592 {
		t.Fatalf("fixture base64 size = %d; want between Go and upstream defaults", encodedSize)
	}
	result := ProcessImage(data, "image/png", nil)
	if !result.OK || result.MimeType != "image/png" || len(result.Hints) != 0 ||
		result.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("ProcessImage result = ok:%v mime:%q hints:%v; want unchanged PNG pass-through", result.OK, result.MimeType, result.Hints)
	}
}

// TestResizeInlineImagePassesThroughWithinLimits pins the early return: bytes
// inside every limit stay byte-identical.
func TestResizeInlineImagePassesThroughWithinLimits(t *testing.T) {
	data := solidPNG(t, 40, 30)
	resized := resizeInlineImage(data, "image/png", DefaultImageResizeOptions)
	if resized == nil {
		t.Fatal("an image within the limits should pass through")
	}
	if resized.WasResized {
		t.Error("WasResized = true for an image within the limits")
	}
	if resized.Data != base64.StdEncoding.EncodeToString(data) {
		t.Error("pass-through did not keep the bytes")
	}
	if note := FormatDimensionNote(resized); note != "" {
		t.Errorf("an unresized image should carry no dimension note, got %q", note)
	}
}

// TestResizeInlineImageShrinksToFitTheByteBudget covers the second half of the
// strategy: when the scaled first candidate is still too large, the loop reduces
// the dimensions until something fits.
func TestResizeInlineImageShrinksToFitTheByteBudget(t *testing.T) {
	// The property is scale-invariant: a noisy image whose first candidate
	// exceeds the budget must be shrunk until one fits. Keep the pixel count
	// (and so the encode cost) to the smallest size that still forces several
	// iterations instead of a production-scale image.
	options := DefaultImageResizeOptions
	options.MaxBytes = 5_000
	resized := resizeInlineImage(noisyPNG(t, 400, 300), "image/png", options)
	if resized == nil {
		t.Fatal("a noisy 400x300 image could not be fitted into 5kB")
	}
	if int64(len(resized.Data)) >= options.MaxBytes {
		t.Fatalf("result is %d bytes, not below the %d budget", len(resized.Data), options.MaxBytes)
	}
	if resized.Width > 400 || resized.Width == 400 && resized.Height == 300 {
		t.Errorf("dimensions %dx%d did not shrink", resized.Width, resized.Height)
	}
}

// Pinned image-resize-core.ts catches Photon resize failures and returns null;
// a non-positive target must not become a successful invalid-dimension image.
func TestProcessImageInvalidResizeDimensionsReturnOmission(t *testing.T) {
	options := ImageResizeOptions{MaxWidth: -1, MaxHeight: 2, MaxBytes: 100_000, JPEGQuality: 80}
	result := ProcessImage(solidPNG(t, 2, 2), "image/png", &ProcessImageOptions{ResizeOptions: &options})
	if result.OK || !strings.Contains(result.Message, "could not be resized") {
		t.Fatalf("result = %+v; want the normal resize omission", result)
	}
}

func TestResizeInlineImageRejectsUndecodableBytes(t *testing.T) {
	if resized := resizeInlineImage([]byte("not an image"), "image/png", DefaultImageResizeOptions); resized != nil {
		t.Fatalf("undecodable bytes produced %#v", resized)
	}
}

// Upstream uses scale.toFixed(2), which rounds half away from zero.
func TestFormatDimensionNoteUsesJSFixed(t *testing.T) {
	note := FormatDimensionNote(&ResizedImage{
		OriginalWidth: 9, OriginalHeight: 9, Width: 8, Height: 8, WasResized: true,
	})
	if !strings.Contains(note, "Multiply coordinates by 1.13") {
		t.Fatalf("note = %q", note)
	}
}

// Pinned v1.5.0 image-resize-core.ts prefers the smaller of PNG and JPEG at the
// configured quality before falling back to lower JPEG qualities; the port used
// to take PNG whenever it fit.
func TestResizeInlineImagePrefersSmallerFormat(t *testing.T) {
	data := noisyPNG(t, 128, 128)
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	scaled := scaleImageBilinear(source, 64, 64)
	var pngBuffer, jpegBuffer bytes.Buffer
	if err := png.Encode(&pngBuffer, scaled); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegBuffer, scaled, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if jpegBuffer.Len() >= pngBuffer.Len() {
		t.Fatalf("fixture needs JPEG smaller than PNG (jpeg=%d png=%d)", jpegBuffer.Len(), pngBuffer.Len())
	}
	options := ImageResizeOptions{MaxWidth: 64, MaxHeight: 64, MaxBytes: 1 << 20, JPEGQuality: 80}
	resized := resizeInlineImage(data, "image/png", options)
	if resized == nil {
		t.Fatal("image was not resized")
	}
	if resized.MimeType != "image/jpeg" {
		t.Fatalf("mime = %q; want the smaller image/jpeg", resized.MimeType)
	}
}

// TestResizeInlineImageRefusesAnOversizedCanvas covers the inline pixel bound
// (D408). A 33-byte PNG can declare a 30000x30000 canvas, which Go allocates as
// 3.4 GiB before it discovers the pixel data is missing; a larger one kills the
// process with a fatal out-of-memory instead of reaching the omission message.
// The header is read with image.DecodeConfig first so the canvas is bounded
// before any decoder allocates it.
func TestResizeInlineImageRefusesAnOversizedCanvas(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("decoding a hostile header panicked: %v", recovered)
		}
	}()
	raw := hugePNGHeaderForTest(30000, 30000)
	if len(raw) > 64 {
		t.Fatalf("fixture is %d bytes, want a small hostile header", len(raw))
	}
	// The bound is checked at the decoder seam, so the error names it. Go's own
	// decoders happen to reject these truncated streams before allocating, so the
	// guard is defence in depth; this asserts it stays wired rather than that a
	// decoder would otherwise exhaust memory.
	_, err := decodeInlineImage(raw)
	if err == nil || !strings.Contains(err.Error(), "exceeds the 100000000-pixel inline limit") {
		t.Fatalf("decodeInlineImage err = %v, want the pixel-limit refusal", err)
	}
	if got := resizeInlineImage(raw, "image/png", DefaultImageResizeOptions); got != nil {
		t.Fatalf("resizeInlineImage returned %+v, want nil for an oversize canvas", got)
	}
	result := ProcessImage(raw, "image/png", nil)
	if result.OK || !strings.Contains(result.Message, "could not be resized") {
		t.Fatalf("ProcessImage = %+v, want the omission message", result)
	}
	// A normal image below the bound still decodes.
	fine := noisyPNG(t, 8, 8)
	if _, err := decodeInlineImage(fine); err != nil {
		t.Fatalf("decodeInlineImage(8x8) err = %v", err)
	}
}

// hugePNGHeaderForTest builds a PNG that declares width x height with no pixel
// data, the shape of a decompression bomb.
func hugePNGHeaderForTest(width, height uint32) []byte {
	var buffer bytes.Buffer
	buffer.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	writeChunk := func(chunkType string, data []byte) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(data)))
		buffer.Write(length[:])
		buffer.WriteString(chunkType)
		buffer.Write(data)
		checksum := crc32.NewIEEE()
		checksum.Write([]byte(chunkType))
		checksum.Write(data)
		var sum [4]byte
		binary.BigEndian.PutUint32(sum[:], checksum.Sum32())
		buffer.Write(sum[:])
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], width)
	binary.BigEndian.PutUint32(header[4:], height)
	header[8] = 8 // bit depth
	writeChunk("IHDR", header)
	return buffer.Bytes()
}
