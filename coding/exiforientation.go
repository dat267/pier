package coding

// Port of utils/exif-orientation.ts: read the EXIF orientation of a JPEG or
// WebP and apply it to a decoded image. Go's image decoders ignore EXIF, so a
// rotated phone photo would otherwise embed sideways.

import "image"

// ExifOrientation returns the EXIF orientation (1..8) of a JPEG or WebP, or 1
// when absent or unreadable.
func ExifOrientation(data []byte) int {
	tiff := -1
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8:
		tiff = findJPEGTIFFOffset(data)
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		tiff = findWebPTIFFOffset(data)
	}
	if tiff < 0 {
		return 1
	}
	return readTIFFOrientation(data, tiff)
}

func hasExifHeader(data []byte, offset int) bool {
	if offset < 0 || offset+6 > len(data) {
		return false
	}
	return data[offset] == 'E' && data[offset+1] == 'x' && data[offset+2] == 'i' &&
		data[offset+3] == 'f' && data[offset+4] == 0 && data[offset+5] == 0
}

func findJPEGTIFFOffset(data []byte) int {
	offset := 2
	for offset < len(data)-1 {
		if data[offset] != 0xFF {
			return -1
		}
		marker := data[offset+1]
		if marker == 0xFF {
			offset++
			continue
		}
		if marker == 0xE1 {
			if offset+4 >= len(data) {
				return -1
			}
			segmentStart := offset + 4
			if segmentStart+6 > len(data) {
				return -1
			}
			if hasExifHeader(data, segmentStart) {
				return segmentStart + 6
			}
		}
		if offset+4 > len(data) {
			return -1
		}
		length := int(data[offset+2])<<8 | int(data[offset+3])
		offset += 2 + length
	}
	return -1
}

func findWebPTIFFOffset(data []byte) int {
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(data[offset+4]) | int(data[offset+5])<<8 | int(data[offset+6])<<16 | int(data[offset+7])<<24
		dataStart := offset + 8
		if chunkID == "EXIF" {
			if dataStart+chunkSize > len(data) {
				return -1
			}
			if chunkSize >= 6 && hasExifHeader(data, dataStart) {
				return dataStart + 6
			}
			return dataStart
		}
		// RIFF chunks are padded to an even size.
		offset = dataStart + chunkSize + (chunkSize % 2)
	}
	return -1
}

func readTIFFOrientation(data []byte, tiffStart int) int {
	if tiffStart+8 > len(data) {
		return 1
	}
	byteOrder := int(data[tiffStart])<<8 | int(data[tiffStart+1])
	littleEndian := byteOrder == 0x4949

	read16 := func(pos int) int {
		if littleEndian {
			return int(data[pos]) | int(data[pos+1])<<8
		}
		return int(data[pos])<<8 | int(data[pos+1])
	}
	read32 := func(pos int) int {
		if littleEndian {
			return int(data[pos]) | int(data[pos+1])<<8 | int(data[pos+2])<<16 | int(data[pos+3])<<24
		}
		return int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	}

	ifdOffset := read32(tiffStart + 4)
	ifdStart := tiffStart + ifdOffset
	if ifdStart < 0 || ifdStart+2 > len(data) {
		return 1
	}
	entryCount := read16(ifdStart)
	for index := 0; index < entryCount; index++ {
		entryPos := ifdStart + 2 + index*12
		if entryPos+12 > len(data) {
			return 1
		}
		if read16(entryPos) == 0x0112 {
			value := read16(entryPos + 8)
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

// ApplyExifOrientation returns the image oriented per its EXIF orientation
// (upstream applyExifOrientation); orientation 1 or an unknown value returns it
// unchanged.
func ApplyExifOrientation(img image.Image, orientation int) image.Image {
	if img == nil || orientation <= 1 || orientation > 8 {
		return img
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dstWidth, dstHeight := width, height
	if orientation >= 5 {
		dstWidth, dstHeight = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for sourceY := 0; sourceY < height; sourceY++ {
		for sourceX := 0; sourceX < width; sourceX++ {
			var destX, destY int
			switch orientation {
			case 2:
				destX, destY = width-1-sourceX, sourceY
			case 3:
				destX, destY = width-1-sourceX, height-1-sourceY
			case 4:
				destX, destY = sourceX, height-1-sourceY
			case 5:
				destX, destY = sourceY, sourceX
			case 6:
				destX, destY = height-1-sourceY, sourceX
			case 7:
				destX, destY = height-1-sourceY, width-1-sourceX
			case 8:
				destX, destY = sourceY, width-1-sourceX
			}
			dst.Set(destX, destY, img.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY))
		}
	}
	return dst
}
