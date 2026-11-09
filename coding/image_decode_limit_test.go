package coding

import (
	"encoding/binary"
	"hash/crc32"
	"runtime"
	"testing"
)

// oversizeCanvasPNG rewrites a 1x1 PNG's IHDR to declare width x height while
// keeping the file itself tiny and every chunk checksum valid, so nothing but
// the declared canvas makes it expensive.
func oversizeCanvasPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()
	data := pngBytes(t, 1, 1)
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// TestProcessImageRejectsAnOversizeImageCanvas pins that a declared canvas is
// bounded before a decoder allocates it. Upstream decodes through Photon inside
// a try/catch (utils/image-resize-core.ts resizeImageInProcess returns null on
// any throw), so an unallocatable canvas becomes the omission message in
// utils/image-process.ts processImage. Go's decoders allocate the whole canvas
// from the header first, which turned a 196-byte PNG into 3.4 GiB and, above
// that, a fatal out-of-memory instead of an omission (pi v1.5.0 f9a26365).
func TestProcessImageRejectsAnOversizeImageCanvas(t *testing.T) {
	data := oversizeCanvasPNG(t, 30000, 30000)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result := ProcessImage(data, "image/png", nil)
	runtime.ReadMemStats(&after)
	if result.OK || result.Message != "[Image omitted: could not be resized below the inline image size limit.]" {
		t.Fatalf("result = %+v; want the resize omission message", result)
	}
	// A 30000x30000 canvas is 3.6 GiB as RGBA. Staying far below that proves the
	// decoder never allocated it.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 256<<20 {
		t.Errorf("decoding a %d-byte PNG allocated %d MiB; the canvas was allocated before it was rejected",
			len(data), allocated>>20)
	}
}

// TestDecodeInlineImageKeepsNormalImages is the control: the canvas bound must
// not reject images the pipeline already accepts.
func TestDecodeInlineImageKeepsNormalImages(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {3000, 100}} {
		source, err := decodeInlineImage(pngBytes(t, size[0], size[1]))
		if err != nil {
			t.Fatalf("%dx%d: %v", size[0], size[1], err)
		}
		if bounds := source.Bounds(); bounds.Dx() != size[0] || bounds.Dy() != size[1] {
			t.Errorf("%dx%d decoded as %v", size[0], size[1], bounds)
		}
	}
}
