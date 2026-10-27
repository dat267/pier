package tui

// Port of pi v1.4.0 packages/tui/test/terminal-image.test.ts: the inline-image
// encoders, dimension parsers, cell-size math, the kitty placement registry and
// the text fallback.

import (
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var osc8StripPattern = regexp.MustCompile("\x1b\\]8;;[^\x1b]*\x1b\\\\")

func withImageCapabilities(t *testing.T, images string, hyperlinks bool) {
	t.Helper()
	previous := GetTerminalCapabilities()
	previousCells := GetCellDimensions()
	t.Cleanup(func() {
		SetTerminalCapabilities(previous)
		SetCellDimensions(previousCells)
	})
	SetTerminalCapabilities(TerminalCapabilities{Images: images, TrueColor: true, Hyperlinks: hyperlinks})
}

func TestIterm2EncodingIncludesDecodedSize(t *testing.T) {
	sequence := EncodeITerm2("AAAA", Iterm2EncodeOptions{Width: "2", HasWidth: true, Height: "auto"})
	want := "\x1b]1337;File=inline=1;size=3;width=2;height=auto:AAAA\x07"
	if sequence != want {
		t.Fatalf("sequence = %q want %q", sequence, want)
	}
}

func TestKittyEncodingCursorMovement(t *testing.T) {
	move := false
	sequence := EncodeKitty("AAAA", KittyEncodeOptions{Columns: 2, HasColumns: true, Rows: 2, HasRows: true, MoveCursor: &move})
	if !strings.HasPrefix(sequence, "\x1b_Ga=T,f=100,q=2,C=1,c=2,r=2;") {
		t.Fatalf("sequence = %q", sequence)
	}
}

func TestKittyPlacementRowsFromControls(t *testing.T) {
	move := false
	sequence := EncodeKitty("AAAA", KittyEncodeOptions{Columns: 2, HasColumns: true, Rows: 3, HasRows: true, MoveCursor: &move})
	rows, ok := GetKittyImagePlacementRows(sequence)
	if !ok || rows != 3 {
		t.Fatalf("rows = %d ok = %v", rows, ok)
	}
}

func TestKittyDeleteSequences(t *testing.T) {
	if got := DeleteKittyImage(42); got != "\x1b_Ga=d,d=I,i=42,q=2\x1b\\" {
		t.Fatalf("deleteKittyImage = %q", got)
	}
	if got := DeleteAllKittyImages(); got != "\x1b_Ga=d,d=A,q=2\x1b\\" {
		t.Fatalf("deleteAllKittyImages = %q", got)
	}
	if got := DeleteAllKittyPlacements(); got != "\x1b_Ga=d,d=a,q=2\x1b\\" {
		t.Fatalf("deleteAllKittyPlacements = %q", got)
	}
}

func TestRenderImageDefaultCursorMovement(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 20, HeightPx: 20}, ImageRenderOptions{MaxWidthCells: 2})
	if !ok {
		t.Fatal("renderImage failed")
	}
	if strings.Contains(result.Sequence, ",C=1,") {
		t.Fatalf("unexpected C=1: %q", result.Sequence)
	}
	if result.Rows != 2 {
		t.Fatalf("rows = %d", result.Rows)
	}
}

func TestRenderImageNoCursorMovement(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	move := false
	result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 20, HeightPx: 20}, ImageRenderOptions{MaxWidthCells: 2, MoveCursor: &move})
	if !ok || !strings.Contains(result.Sequence, ",C=1,") || result.Rows != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRenderImageRegistersMetadataAndCrops(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	move := false
	result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 100, HeightPx: 100}, ImageRenderOptions{
		MaxWidthCells: 3, ImageID: 42, HasImageID: true, MoveCursor: &move,
	})
	if !ok {
		t.Fatal("renderImage failed")
	}
	metadata, ok := GetKittyImageMetadata(result.Sequence)
	if !ok || metadata != (KittyImageMetadata{ImageID: 42, Columns: 3, Rows: 3, WidthPx: 100, HeightPx: 100}) {
		t.Fatalf("metadata = %+v ok = %v", metadata, ok)
	}
	if cropped := CropKittyImageLine(result.Sequence, 2, 1); !strings.Contains(cropped, "y=66,h=34,r=1") {
		t.Fatalf("cropped = %q", cropped)
	}
}

func TestGetKittyImagePlacementForUploadedImage(t *testing.T) {
	RegisterKittyImageMetadata(KittyImageMetadata{ImageID: 42, Columns: 3, Rows: 3, WidthPx: 100, HeightPx: 100})
	move := false
	transmission := EncodeKitty(strings.Repeat("A", 8192), KittyEncodeOptions{
		Columns: 3, HasColumns: true, Rows: 3, HasRows: true, ImageID: 42, HasImageID: true, MoveCursor: &move,
	})
	line := "left " + CropKittyImageLine(transmission, 2, 1) + " right"
	placement, ok := GetKittyImagePlacement(line)
	if !ok {
		t.Fatal("no placement")
	}
	if rows, ok := GetKittyImagePlacementRows(line); !ok || rows != 1 {
		t.Fatalf("rows = %d ok = %v", rows, ok)
	}
	if placement.TransmissionBytes != len(line)-len("left ")-len(" right") {
		t.Fatalf("transmissionBytes = %d", placement.TransmissionBytes)
	}
	if placement.EstimatedDecodedBytes != 100*100*4 {
		t.Fatalf("estimatedDecodedBytes = %d", placement.EstimatedDecodedBytes)
	}
	if placement.Rows != 1 {
		t.Fatalf("placement.Rows = %d", placement.Rows)
	}
	wantSequence := "\x1b_Ga=p,q=2,C=1,c=3,i=42,y=66,h=34,r=1\x1b\\"
	if placement.Sequence != wantSequence {
		t.Fatalf("sequence = %q want %q", placement.Sequence, wantSequence)
	}
	if placement.ReplacementLine != "left "+placement.Sequence+" right" {
		t.Fatalf("replacementLine = %q", placement.ReplacementLine)
	}
	if strings.Contains(placement.ReplacementLine, "AAAA") {
		t.Fatalf("replacementLine kept the payload: %q", placement.ReplacementLine)
	}
}

func TestRenderImageHonorsMaxHeight(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	maxHeight := 5
	result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 10, HeightPx: 100}, ImageRenderOptions{MaxWidthCells: 10, MaxHeightCells: &maxHeight})
	if !ok || result.Rows != 5 || !strings.Contains(result.Sequence, ",c=1,r=5") {
		t.Fatalf("result = %+v", result)
	}
}

func TestImageComponentCapsHeightToSquareBox(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 20})
	maxWidth := 10
	image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return value }},
		ImageOptions{MaxWidthCells: &maxWidth}, &ImageDimensions{WidthPx: 10, HeightPx: 100})
	lines := image.Render(12)
	if len(lines) != 5 || !strings.Contains(lines[0], ",c=1,r=5") {
		t.Fatalf("lines = %d %q", len(lines), lines)
	}
}

func TestImageComponentPlacesSequenceOnFirstLine(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	maxWidth := 2
	image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return value }},
		ImageOptions{MaxWidthCells: &maxWidth}, &ImageDimensions{WidthPx: 20, HeightPx: 20})
	lines := image.Render(4)
	imageID, ok := image.GetImageID()
	if !ok {
		t.Fatal("no image id")
	}
	if !strings.HasPrefix(lines[0], "\x1b_G") || !strings.Contains(lines[0], ",C=1,") ||
		!strings.Contains(lines[0], ",i="+strconv.Itoa(imageID)) || !strings.HasSuffix(lines[0], "\x1b\\") {
		t.Fatalf("first line = %q", lines[0])
	}
	if len(lines) != 2 || lines[1] != "" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestImageComponentTruncatesLongFallback(t *testing.T) {
	withImageCapabilities(t, "", false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	longPath := filepath.Join(home, "images", strings.Repeat("generated-image-with-a-very-long-absolute-path", 4)+".png")
	width := 40
	image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return "\x1b[33m" + value + "\x1b[0m" }},
		ImageOptions{Filename: longPath}, &ImageDimensions{WidthPx: 1280, HeightPx: 720})
	lines := image.Render(width)
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	if VisibleWidth(lines[0]) > width {
		t.Fatalf("fallback wider than %d: %q", width, lines[0])
	}
	if !strings.Contains(lines[0], "...") || !strings.Contains(lines[0], "~") {
		t.Fatalf("fallback = %q", lines[0])
	}
}

func TestImageCellSizingKitty(t *testing.T) {
	withImageCapabilities(t, ImageProtocolKitty, true)

	t.Run("thin image reserves one row", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 9, HeightPx: 18})
		result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 1200, HeightPx: 12}, ImageRenderOptions{MaxWidthCells: 60})
		if !ok || result.Rows != 1 || !strings.Contains(result.Sequence, ",c=60,r=1;") {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("width changes keep placement and crop metadata consistent", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 9, HeightPx: 18})
		maxWidth := 60
		imageID := 8938
		image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return value }},
			ImageOptions{MaxWidthCells: &maxWidth, ImageID: &imageID}, &ImageDimensions{WidthPx: 615, HeightPx: 86})
		lines := image.Render(62)
		if len(lines) != 4 || lines[1] != "" || lines[2] != "" || lines[3] != "" {
			t.Fatalf("lines = %q", lines)
		}
		if !strings.Contains(lines[0], ",c=60,r=4,i=8938;") {
			t.Fatalf("first line = %q", lines[0])
		}
		metadata, ok := GetKittyImageMetadata(lines[0])
		if !ok || metadata != (KittyImageMetadata{ImageID: 8938, Columns: 60, Rows: 4, WidthPx: 615, HeightPx: 86}) {
			t.Fatalf("metadata = %+v ok = %v", metadata, ok)
		}
		cropped := CropKittyImageLine(lines[0], 1, 2)
		placement, ok := GetKittyImagePlacement(cropped)
		if !ok || placement.Sequence != "\x1b_Ga=p,q=2,C=1,c=60,i=8938,y=21,h=44,r=2\x1b\\" {
			t.Fatalf("placement = %+v ok = %v", placement, ok)
		}
		narrower := image.Render(32)
		if len(narrower) != 2 || !strings.Contains(narrower[0], ",c=30,r=2,i=8938;") {
			t.Fatalf("narrower = %q", narrower)
		}
		if metadata, ok := GetKittyImageMetadata(narrower[0]); !ok || metadata.Rows != 2 {
			t.Fatalf("narrower metadata = %+v ok = %v", metadata, ok)
		}
	})

	t.Run("keeps ceiling placement when rounding down would distort more", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 15, HeightPx: 28})
		result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 615, HeightPx: 86}, ImageRenderOptions{MaxWidthCells: 60})
		if !ok || result.Rows != 5 || !strings.Contains(result.Sequence, ",c=60,r=5;") {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("height-limited columns stay consistent", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 14, HeightPx: 28})
		maxWidth := 30
		imageID := 8938
		image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return value }},
			ImageOptions{MaxWidthCells: &maxWidth, ImageID: &imageID}, &ImageDimensions{WidthPx: 400, HeightPx: 900})
		lines := image.Render(32)
		if len(lines) != 15 || !strings.Contains(lines[0], ",c=13,r=15,i=8938;") {
			t.Fatalf("lines = %d first = %q", len(lines), lines[0])
		}
		cropped := CropKittyImageLine(lines[0], 1, 2)
		placement, ok := GetKittyImagePlacement(cropped)
		if !ok || placement.Sequence != "\x1b_Ga=p,q=2,C=1,c=13,i=8938,y=60,h=120,r=2\x1b\\" {
			t.Fatalf("placement = %+v ok = %v", placement, ok)
		}
		narrower := image.Render(22)
		if len(narrower) != 10 || !strings.Contains(narrower[0], ",c=9,r=10,i=8938;") {
			t.Fatalf("narrower = %d %q", len(narrower), narrower[0])
		}
	})

	t.Run("thin widths choose by proportions", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 1, HeightPx: 1})
		maxHeight := 10
		for _, testCase := range []struct {
			widthPx int
			columns int
		}{{1, 1}, {140, 1}, {149, 2}} {
			result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: testCase.widthPx, HeightPx: 1000}, ImageRenderOptions{
				MaxWidthCells: 30, MaxHeightCells: &maxHeight,
			})
			if !ok || result.Columns != testCase.columns || result.Rows != 10 {
				t.Fatalf("widthPx %d: result = %+v", testCase.widthPx, result)
			}
			if !strings.Contains(result.Sequence, ",c="+strconv.Itoa(testCase.columns)+",r=10;") {
				t.Fatalf("widthPx %d: sequence = %q", testCase.widthPx, result.Sequence)
			}
		}
	})
}

func TestImageCellSizingITerm2(t *testing.T) {
	withImageCapabilities(t, ImageProtocolITerm2, true)

	t.Run("ceiling width when height-limited", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 14, HeightPx: 28})
		maxHeight := 15
		result, ok := RenderImage("AAAA", ImageDimensions{WidthPx: 400, HeightPx: 900}, ImageRenderOptions{MaxWidthCells: 30, MaxHeightCells: &maxHeight})
		if !ok || result.Columns != 14 || result.Rows != 15 {
			t.Fatalf("result = %+v", result)
		}
		want := "\x1b]1337;File=inline=1;size=3;width=14;height=auto:AAAA\x07"
		if result.Sequence != want {
			t.Fatalf("sequence = %q want %q", result.Sequence, want)
		}
	})

	t.Run("reserved lines and cursor offset", func(t *testing.T) {
		SetCellDimensions(CellDimensions{WidthPx: 9, HeightPx: 18})
		maxWidth := 60
		image := NewImageComponent("AAAA", "image/png", ImageTheme{FallbackColor: func(value string) string { return value }},
			ImageOptions{MaxWidthCells: &maxWidth}, &ImageDimensions{WidthPx: 615, HeightPx: 86})
		lines := image.Render(62)
		wantLast := "\x1b[4A\x1b]1337;File=inline=1;size=3;width=60;height=auto:AAAA\x07"
		if len(lines) != 5 || lines[0] != "" || lines[3] != "" || lines[4] != wantLast {
			t.Fatalf("lines = %q", lines)
		}
	})
}

func TestImageFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("shortens home paths without hyperlinks", func(t *testing.T) {
		withImageCapabilities(t, "", false)
		absolute := filepath.Join(home, ".pi", "agent", "shot.png")
		result := ImageFallback("image/png", &ImageDimensions{WidthPx: 1280, HeightPx: 720}, absolute)
		if result != "[Image: ~/.pi/agent/shot.png [image/png] 1280x720]" {
			t.Fatalf("fallback = %q", result)
		}
	})

	t.Run("wraps absolute paths in OSC 8 when hyperlinks are enabled", func(t *testing.T) {
		withImageCapabilities(t, "", true)
		absolute := filepath.Join(home, ".pi", "agent", "shot.png")
		result := ImageFallback("image/png", &ImageDimensions{WidthPx: 10, HeightPx: 10}, absolute)
		if !strings.Contains(result, "\x1b]8;;file://") {
			t.Fatalf("fallback = %q", result)
		}
		visible := osc8StripPattern.ReplaceAllString(result, "")
		if visible != "[Image: ~/.pi/agent/shot.png [image/png] 10x10]" {
			t.Fatalf("visible = %q", visible)
		}
	})

	t.Run("leaves bare basenames unlinked", func(t *testing.T) {
		withImageCapabilities(t, "", true)
		result := ImageFallback("image/png", &ImageDimensions{WidthPx: 1, HeightPx: 1}, "clankolas.png")
		if result != "[Image: clankolas.png [image/png] 1x1]" || strings.Contains(result, "\x1b]8;") {
			t.Fatalf("fallback = %q", result)
		}
	})

	t.Run("omits the filename segment when absent", func(t *testing.T) {
		withImageCapabilities(t, "", false)
		if got := ImageFallback("image/png", &ImageDimensions{WidthPx: 8, HeightPx: 6}, ""); got != "[Image: [image/png] 8x6]" {
			t.Fatalf("fallback = %q", got)
		}
	})
}

func TestHyperlinkSequence(t *testing.T) {
	if got := Hyperlink("click me", "https://example.com"); got != "\x1b]8;;https://example.com\x1b\\click me\x1b]8;;\x1b\\" {
		t.Fatalf("hyperlink = %q", got)
	}
}

func TestImageTranscoding(t *testing.T) {
	jpeg := base64.StdEncoding.EncodeToString([]byte("jpeg"))
	pngBytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAACgAAAAK")
	if err != nil {
		t.Fatal(err)
	}
	png := base64.StdEncoding.EncodeToString(pngBytes)
	var calls []string
	render := func(data, mimeType string) []string {
		return NewImageComponent(data, mimeType, ImageTheme{FallbackColor: func(value string) string { return value }}, ImageOptions{}, &ImageDimensions{WidthPx: 20, HeightPx: 20}).Render(20)
	}

	withImageCapabilities(t, ImageProtocolKitty, true)
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})
	t.Cleanup(func() { SetImageTranscoder(nil) })

	t.Run("sends converted PNG data sized from the PNG", func(t *testing.T) {
		SetImageTranscoder(func(data, mimeType string) (string, bool) {
			calls = append(calls, data)
			if data == jpeg {
				return png, true
			}
			return "", false
		})
		lines := render(jpeg, "image/jpeg")
		if !strings.Contains(lines[0], "f=100") || !strings.Contains(lines[0], ";"+png+"\x1b\\") {
			t.Fatalf("first line = %q", lines[0])
		}
		if len(lines) != 5 {
			t.Fatalf("lines = %d", len(lines))
		}
	})

	t.Run("renders a fallback until a working transcoder is registered", func(t *testing.T) {
		SetImageTranscoder(nil)
		image := NewImageComponent(jpeg, "image/jpeg", ImageTheme{FallbackColor: func(value string) string { return value }}, ImageOptions{}, nil)
		if !strings.HasPrefix(image.Render(80)[0], "[Image: [image/jpeg]") {
			t.Fatalf("fallback = %q", image.Render(80)[0])
		}
		SetImageTranscoder(func(data, mimeType string) (string, bool) { return "", false })
		image.Invalidate()
		if !strings.HasPrefix(image.Render(80)[0], "[Image: [image/jpeg]") {
			t.Fatalf("fallback = %q", image.Render(80)[0])
		}
		SetImageTranscoder(func(data, mimeType string) (string, bool) { return png, true })
		image.Invalidate()
		if !strings.Contains(image.Render(80)[0], "\x1b_G") {
			t.Fatalf("expected kitty sequence: %q", image.Render(80)[0])
		}
	})

	t.Run("converts each image once", func(t *testing.T) {
		calls = nil
		SetImageTranscoder(func(data, mimeType string) (string, bool) {
			calls = append(calls, data)
			if data == jpeg {
				return png, true
			}
			return "", false
		})
		image := NewImageComponent(jpeg, "image/jpeg", ImageTheme{FallbackColor: func(value string) string { return value }}, ImageOptions{}, nil)
		image.Render(80)
		render(jpeg, "image/jpeg") // New instance hits the shared cache.
		for index := range 40 {
			render("other-"+strconv.Itoa(index), "image/jpeg")
		}
		image.Invalidate()
		image.Render(40) // The instance keeps its own PNG.
		count := 0
		for _, call := range calls {
			if call == jpeg {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("transcoder calls for the jpeg = %d", count)
		}
	})

	t.Run("does not convert PNG data or iTerm2 output", func(t *testing.T) {
		calls = nil
		SetImageTranscoder(func(data, mimeType string) (string, bool) {
			calls = append(calls, data)
			return "", false
		})
		if !strings.Contains(render(png, "image/png")[0], ";"+png+"\x1b\\") {
			t.Fatal("png data should not be converted")
		}
		SetTerminalCapabilities(TerminalCapabilities{Images: ImageProtocolITerm2, TrueColor: true, Hyperlinks: true})
		lines := render(jpeg, "image/jpeg")
		if !strings.HasSuffix(lines[len(lines)-1], ":"+jpeg+"\x07") {
			t.Fatalf("iterm2 line = %q", lines[len(lines)-1])
		}
		if len(calls) != 0 {
			t.Fatalf("transcoder calls = %v", calls)
		}
	})
}

func TestGetImageDimensionsFormats(t *testing.T) {
	// PNG 40x10 (signature + IHDR), JPEG SOF0, GIF89a, WebP VP8X.
	pngData := mustBase64(t, "89504e470d0a1a0a0000000d49484452000000280000000a")
	if dims, ok := GetImageDimensions(pngData, "image/png"); !ok || dims != (ImageDimensions{WidthPx: 40, HeightPx: 10}) {
		t.Fatalf("png = %+v ok = %v", dims, ok)
	}
	gif := append([]byte("GIF89a"), 0x20, 0x00, 0x10, 0x00)
	if dims, ok := GetImageDimensions(base64.StdEncoding.EncodeToString(gif), "image/gif"); !ok || dims != (ImageDimensions{WidthPx: 32, HeightPx: 16}) {
		t.Fatalf("gif = %+v ok = %v", dims, ok)
	}
	if _, ok := GetImageDimensions(pngData, "image/jpeg"); ok {
		t.Fatal("png data decoded as jpeg")
	}
}

func mustBase64(t *testing.T, hexPayload string) string {
	t.Helper()
	buffer, err := hex.DecodeString(hexPayload)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buffer)
}

type recordingTerminalWriter struct{ data strings.Builder }

func (w *recordingTerminalWriter) Write(text string) { w.data.WriteString(text) }

func TestParseCellSizeResponse(t *testing.T) {
	size, ok := ParseCellSizeResponse("\x1b[6;36;18t")
	if !ok || size != (CellDimensions{WidthPx: 18, HeightPx: 36}) {
		t.Fatalf("size = %+v ok = %v", size, ok)
	}
	for _, invalid := range []string{"plain", "\x1b[6;36t", "\x1b[8;1;1t"} {
		if _, ok := ParseCellSizeResponse(invalid); ok {
			t.Fatalf("%q parsed as a cell-size reply", invalid)
		}
	}
	// A zero dimension still parses (the reply is consumed); the renderer path
	// refuses to apply it.
	if size, ok := ParseCellSizeResponse("\x1b[6;0;18t"); !ok || size.HeightPx != 0 {
		t.Fatalf("zero reply = %+v ok = %v", size, ok)
	}
}

func TestQueryCellSizeOnlyWhenImagesAreSupported(t *testing.T) {
	previous := GetTerminalCapabilities()
	t.Cleanup(func() { SetTerminalCapabilities(previous) })
	queries := &terminalQueries{}
	writer := &recordingTerminalWriter{}
	SetTerminalCapabilities(TerminalCapabilities{})
	queries.QueryCellSize(writer)
	if writer.data.String() != "" {
		t.Fatalf("query without images = %q", writer.data.String())
	}
	SetTerminalCapabilities(TerminalCapabilities{Images: ImageProtocolKitty})
	queries.QueryCellSize(writer)
	if writer.data.String() != "\x1b[16t" {
		t.Fatalf("query = %q", writer.data.String())
	}
}
