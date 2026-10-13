package durable

import (
	"encoding/base64"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of packages/durable/src/tools/image.ts and image-processor.ts's limits,
// plus read.ts's image result path at pi v1.2.0.

// ImageLimits bound an image the model sees.
type ImageLimits struct {
	// MaxWidth and MaxHeight are pixels.
	MaxWidth  int
	MaxHeight int
	// MaxBytes is bytes of base64.
	MaxBytes int64
}

// DefaultImageLimits are the coding agent's limits, below Anthropic's 5 MB.
var DefaultImageLimits = ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBytes: 4_500_000}

// InlineImageTypes are the formats every provider takes inline; others, such as
// BMP, need converting.
var InlineImageTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// Base64Length is the length of the base64 of byteLength bytes.
func Base64Length(byteLength int64) int64 {
	if byteLength <= 0 {
		return 0
	}
	return (byteLength + 2) / 3 * 4
}

// ImageDimensions reads the width and height an image of mimeType declares in
// its header, without decoding it; ok is false when the header cannot be read.
func ImageDimensions(bytes []byte, mimeType string) (width, height int, ok bool) {
	switch mimeType {
	case "image/png":
		if len(bytes) < 24 {
			return 0, 0, false
		}
		return int(readUint32BE(bytes, 16)), int(readUint32BE(bytes, 20)), true
	case "image/gif":
		if len(bytes) < 10 {
			return 0, 0, false
		}
		return readUint16LE(bytes, 6), readUint16LE(bytes, 8), true
	case "image/webp":
		if len(bytes) < 30 {
			return 0, 0, false
		}
		if startsWithAscii(bytes, 12, "VP8 ") {
			return readUint16LE(bytes, 26) & 0x3fff, readUint16LE(bytes, 28) & 0x3fff, true
		}
		if startsWithAscii(bytes, 12, "VP8L") {
			bits := readUint32LE(bytes, 21)
			return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, true
		}
		if startsWithAscii(bytes, 12, "VP8X") {
			uint24 := func(offset int) int {
				return readUint16LE(bytes, offset) + (at(bytes, offset+2) << 16)
			}
			return uint24(24) + 1, uint24(27) + 1, true
		}
		return 0, 0, false
	case "image/jpeg":
		for offset := 2; offset+9 <= len(bytes); {
			if bytes[offset] != 0xff {
				return 0, 0, false
			}
			marker := bytes[offset+1]
			if marker == 0xff {
				offset++
				continue
			}
			// SOF0 to SOF15, except DHT (C4), JPG (C8), and DAC (CC).
			if marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc {
				return readUint16BE(bytes, offset+7), readUint16BE(bytes, offset+5), true
			}
			offset += 2 + readUint16BE(bytes, offset+2)
		}
		return 0, 0, false
	default:
		return 0, 0, false
	}
}

func readUint16BE(buffer []byte, offset int) int {
	return (at(buffer, offset) << 8) + at(buffer, offset+1)
}

// ImageModel is what the conversation's model takes: whether images, and within
// which limits.
type ImageModel struct {
	Vision bool
	Limits ImageLimits
}

// imageModelFor resolves the conversation model's image input; without a
// resolvable model it counts as one that takes images within the defaults.
func imageModelFor(api ToolExecutionApi, ctx chord.Context) (ImageModel, error) {
	result := ImageModel{Vision: true, Limits: DefaultImageLimits}
	if api == nil {
		return result, nil
	}
	agent, err := api.Agent(ctx)
	if err != nil {
		return result, err
	}
	if agent.Model == nil {
		return result, nil
	}
	models := api.Models()
	if models == nil {
		return result, nil
	}
	model := models.GetModel(agent.Model.Provider, agent.Model.ModelID)
	if model == nil {
		return result, nil
	}
	result.Vision = false
	for _, input := range model.Input {
		if input == "image" {
			result.Vision = true
		}
	}
	if model.InputLimits != nil && model.InputLimits.Images != nil && model.InputLimits.Images.Resize != nil {
		resize := model.InputLimits.Images.Resize
		if resize.MaxWidth > 0 {
			result.Limits.MaxWidth = int(resize.MaxWidth)
		}
		if resize.MaxHeight > 0 {
			result.Limits.MaxHeight = int(resize.MaxHeight)
		}
		if resize.MaxBytes > 0 {
			result.Limits.MaxBytes = resize.MaxBytes
		}
	}
	return result, nil
}

// readImageResult is the result for an opened image: one image block, prepared
// by the processor when configured, or, without one, as it is when its format
// is inline and it fits the limits; otherwise an error result saying why.
func readImageResult(reader BinaryReader, info *FileInfo, path, mimeType string, model ImageModel, ctx chord.Context) (ToolExecutionResult, error) {
	unshrinkable := "and no image processor is configured to shrink it"
	if !InlineImageTypes[mimeType] {
		return readResultError("unsupported_image",
			fmt.Sprintf("%s is an image (%s) that needs converting, and no image processor is configured", path, mimeType)), nil
	}
	if Base64Length(info.Size) > model.Limits.MaxBytes {
		return readResultError("unsupported_image",
			fmt.Sprintf("%s is an image (%s) of %s, too large to send, %s", path, mimeType, FormatSize(int(info.Size)), unshrinkable)), nil
	}
	bytes, err := reader.Read(0, int(info.Size), ctx)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	width, height, ok := ImageDimensions(bytes, mimeType)
	if !ok {
		return readResultError("unsupported_image",
			fmt.Sprintf("%s is an image (%s) whose size cannot be read", path, mimeType)), nil
	}
	if width > model.Limits.MaxWidth || height > model.Limits.MaxHeight {
		return readResultError("unsupported_image",
			fmt.Sprintf("%s is an image (%s) of %dx%d, larger than %dx%d, %s", path, mimeType, width, height, model.Limits.MaxWidth, model.Limits.MaxHeight, unshrinkable)), nil
	}
	message := fmt.Sprintf("Read image file [%s].", mimeType)
	if !model.Vision {
		message += " The current model does not support images; it sees a placeholder instead."
	}
	code := "image"
	return ToolExecutionResult{
		Content:     []ai.UserContent{ai.ImageContent{Data: base64.StdEncoding.EncodeToString(bytes), MimeType: mimeType}},
		Diagnostics: []ToolDiagnostic{{Severity: "info", Code: &code, Message: message}},
	}, nil
}
