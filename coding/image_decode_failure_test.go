package coding

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

// A tiny PNG declaring a 2^60-byte decoded buffer hits Go's maximum-allocation
// guard before any large allocation. Retain valid chunk checksums and IDAT.
func unallocatablePNG(t *testing.T) []byte {
	t.Helper()
	data := pngBytes(t, 1, 1)
	binary.BigEndian.PutUint32(data[16:20], 1<<29)
	binary.BigEndian.PutUint32(data[20:24], 1<<29)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// pi v1.4.0 (4a080252), utils/image-resize-core.ts resizeImageInProcess
// catches decoder failures and returns null; processImage emits an omission.
func TestProcessImageRejectsUnallocatableDimensions(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("image decoder panic escaped omission handling: %v", recovered)
		}
	}()
	result := ProcessImage(unallocatablePNG(t), "image/png", nil)
	if result.OK || result.Message != "[Image omitted: could not be resized below the inline image size limit.]" {
		t.Fatalf("result = %+v; want image omission", result)
	}
}

// pi v1.4.0 utils/image-convert.ts encodePng catches decoder failures too;
// a non-PNG claimed MIME must not let malicious encoded data escape that guard.
func TestImageTranscodeRejectsUnallocatableDimensions(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("image decoder panic escaped transcode failure: %v", recovered)
		}
	}()
	data := base64.StdEncoding.EncodeToString(unallocatablePNG(t))
	if converted, ok := EncodeImageAsPNG(data, "image/jpeg"); ok || converted != "" {
		t.Fatalf("transcode = %q %v; want conversion failure", converted, ok)
	}
}
