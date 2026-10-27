package tui

// Replays the upstream-generated terminal-image goldens. Regenerate with:
//   node --experimental-strip-types tui/testdata/gen_terminal_image.mjs > tui/testdata/terminal_image_golden.txt

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestTerminalImageGoldens(t *testing.T) {
	data, err := os.ReadFile("testdata/terminal_image_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		label, encoded, ok := strings.Cut(line, " => ")
		if !ok {
			t.Fatalf("bad golden line: %q", line)
		}
		var want any
		if err := json.Unmarshal([]byte(encoded), &want); err != nil {
			t.Fatalf("%s: golden json: %v", label, err)
		}
		got := terminalImageGoldenValue(t, label)
		if !jsonEqual(t, got, want) {
			t.Errorf("%s: got %v want %v", label, got, want)
		}
	}
}

func jsonEqual(t *testing.T, got any, want any) bool {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		t.Fatal(err)
	}
	return jsonValuesEqual(normalized, want)
}

func jsonValuesEqual(a, b any) bool {
	switch left := a.(type) {
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, ok := right[key]
			if !ok || !jsonValuesEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !jsonValuesEqual(left[index], right[index]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func parseDimensions(value string) ImageDimensions {
	width, height, _ := strings.Cut(value, "x")
	widthPx, _ := strconv.Atoi(width)
	heightPx, _ := strconv.Atoi(height)
	return ImageDimensions{WidthPx: widthPx, HeightPx: heightPx}
}

func terminalImageGoldenValue(t *testing.T, label string) any {
	t.Helper()
	if strings.HasPrefix(label, "isImageLine|") {
		_, name, _ := strings.Cut(label, "|")
		return isImageLineGolden(name)
	}
	if strings.HasPrefix(label, "cells|") || strings.HasPrefix(label, "rows|") {
		parts := strings.Split(label, "|")
		if parts[0] == "cells" {
			image := parseDimensions(parts[1])
			maxWidth, _ := strconv.Atoi(parts[2])
			maxHeight, _ := strconv.Atoi(parts[3])
			var maxHeightPtr *int
			if maxHeight >= 0 {
				maxHeightPtr = &maxHeight
			}
			cells := parseDimensions(parts[4])
			optimize := parts[5] == "true"
			size := CalculateImageCellSize(image, maxWidth, maxHeightPtr, CellDimensions(cells), optimize)
			return map[string]int{"columns": size.Columns, "rows": size.Rows}
		}
		image := parseDimensions(parts[1])
		maxWidth, _ := strconv.Atoi(parts[2])
		cells := parseDimensions(parts[3])
		return CalculateImageRows(image, maxWidth, CellDimensions(cells))
	}
	if prefix, name, ok := strings.Cut(label, "|"); ok {
		base64Data := dimensionFixture(t, name)
		var dims ImageDimensions
		var found bool
		switch prefix {
		case "png":
			dims, found = GetPngDimensions(base64Data)
		case "jpeg":
			dims, found = GetJpegDimensions(base64Data)
		case "gif":
			dims, found = GetGifDimensions(base64Data)
		case "webp":
			dims, found = GetWebpDimensions(base64Data)
		default:
			t.Fatalf("unknown golden label %q", label)
		}
		if !found {
			return nil
		}
		return map[string]int{"widthPx": dims.WidthPx, "heightPx": dims.HeightPx}
	}
	move := false
	switch label {
	case "kitty.small":
		return EncodeKitty("AAAA", KittyEncodeOptions{})
	case "kitty.columns-rows":
		return EncodeKitty("AAAA", KittyEncodeOptions{Columns: 2, HasColumns: true, Rows: 3, HasRows: true})
	case "kitty.image-id":
		return EncodeKitty("AAAA", KittyEncodeOptions{ImageID: 42, HasImageID: true})
	case "kitty.no-move":
		return EncodeKitty("AAAA", KittyEncodeOptions{Columns: 2, HasColumns: true, Rows: 2, HasRows: true, MoveCursor: &move})
	case "kitty.chunked":
		return EncodeKitty(strings.Repeat("A", 10000), KittyEncodeOptions{Columns: 5, HasColumns: true, ImageID: 7, HasImageID: true, MoveCursor: &move})
	case "iterm2.basic":
		return EncodeITerm2("AAAA", Iterm2EncodeOptions{})
	case "iterm2.width-height":
		return EncodeITerm2("AAAA", Iterm2EncodeOptions{Width: "2", HasWidth: true, Height: "auto"})
	case "iterm2.name":
		return EncodeITerm2("AAAA", Iterm2EncodeOptions{Name: "pixel.png", HasName: true})
	case "iterm2.no-aspect":
		preserve := false
		return EncodeITerm2("AAAA", Iterm2EncodeOptions{Width: "10", HasWidth: true, PreserveAspectRatio: &preserve})
	case "iterm2.not-inline":
		inline := false
		return EncodeITerm2("AAAA", Iterm2EncodeOptions{Inline: &inline})
	case "delete.image":
		return DeleteKittyImage(42)
	case "delete.all-images":
		return DeleteAllKittyImages()
	case "delete.all-placements":
		return DeleteAllKittyPlacements()
	}
	t.Fatalf("unknown golden label %q", label)
	return nil
}

func isImageLineGolden(name string) any {
	switch name {
	case "iterm2.start":
		return IsImageLine("\x1b]1337;File=size=100,100;inline=1:base64encodeddata==\x07")
	case "iterm2.middle":
		return IsImageLine("Some text \x1b]1337;File=inline=1:data==\x07 more text")
	case "kitty.start":
		return IsImageLine("\x1b_Ga=T,f=100;data\x1b\\\x1b_Gm=i=1;\x1b\\")
	case "kitty.middle":
		return IsImageLine("Output: \x1b_Ga=T,f=100;data\x1b\\")
	case "plain":
		return IsImageLine("This is just a regular text line")
	case "ansi-only":
		return IsImageLine("\x1b[31mRed text\x1b[0m")
	case "partial-iterm2":
		return IsImageLine("Some text with ]1337;File but missing ESC")
	case "partial-kitty":
		return IsImageLine("Some text with _G but missing ESC")
	case "empty":
		return IsImageLine("")
	}
	return nil
}

func dimensionFixture(t *testing.T, name string) string {
	t.Helper()
	hexPayloads := map[string]string{
		"png.40x10": "89504e470d0a1a0a0000000d49484452000000280000000a",
		"jpeg.8x6":  "ffd8ffe000104a46494600010100000100010000ffc00011080006000803012200021101031101ffd9",
		"gif.32x16": "47494638396120001000",
		"webp.2x3":  "52494646b000000057454250565038580a00000001000000000000000200000003",
	}
	payload, ok := hexPayloads[name]
	if !ok {
		t.Fatalf("unknown fixture %q", name)
	}
	return mustBase64(t, payload)
}
