package tui

// Reference: pi v1.5.2 (b4afdddf) packages/tui/src/terminal-image.ts
// (cropKittyImageLine, getKittyImagePlacement, getKittyImageMetadata). Every
// lookup there scans `\x1b_G([^;]*);`, so a kitty command with no controls
// terminator has no metadata, and a crop rewrites the controls in place,
// leaving the original payload after exactly one `;`.

import (
	"strings"
	"testing"
)

func registerCropFixture(t *testing.T) {
	t.Helper()
	RegisterKittyImageMetadata(KittyImageMetadata{ImageID: 7, Columns: 4, Rows: 3, WidthPx: 100, HeightPx: 60})
}

func TestCropKittyImageLineKeepsItsPayloadAfterOneSemicolon(t *testing.T) {
	registerCropFixture(t)
	line := "\x1b_Ga=T,f=100,i=7;AAAA\x1b\\"
	got := CropKittyImageLine(line, 1, 2)
	want := "\x1b_Ga=T,f=100,i=7,y=20,h=40,r=2;AAAA\x1b\\"
	if got != want {
		t.Fatalf("crop = %q, want %q", got, want)
	}
	if placement, ok := GetKittyImagePlacement(got); !ok || !strings.Contains(placement.Sequence, "i=7") {
		t.Fatalf("cropped line has no placement: %v %v", placement, ok)
	}
}

func TestKittyImageLookupsRequireTheControlsTerminator(t *testing.T) {
	registerCropFixture(t)
	// A placement-only command carries no payload and no `;`, which upstream
	// treats as having no metadata; accepting it rewrites the line into
	// `...\x1b\\z\x1b\\z` because the payload offset is then wrong.
	line := "x\x1b_Ga=p,i=7,x=1,y=2,w=3,h=4\x1b\\z"
	if _, ok := GetKittyImageMetadata(line); ok {
		t.Fatalf("metadata found for a command without a controls terminator")
	}
	if _, ok := GetKittyImagePlacementRows(line); ok {
		t.Fatalf("rows found for a command without a controls terminator")
	}
	if placement, ok := GetKittyImagePlacement(line); ok {
		t.Fatalf("placement %+v found for a command without a controls terminator", placement)
	}
	if got := CropKittyImageLine(line, 1, 2); got != line {
		t.Fatalf("crop = %q, want the line unchanged", got)
	}
}

// FuzzKittyImageScans keeps the offset arithmetic in the kitty scans honest:
// each lookup slices the line at regexp positions, and an image line arrives
// from the renderer or from the terminal, so a bad offset is a crash.
func FuzzKittyImageScans(f *testing.F) {
	f.Add("")
	f.Add("\x1b_Ga=T,f=100,i=7;AAAA\x1b\\")
	f.Add("\x1b_Ga=T,f=100,i=7,m=1;AAAA\x1b\\\x1b_Gm=1;BBBB\x1b\\")
	f.Add("text \x1b_Ga=p,i=1,x=1,y=2,w=3,h=4\x1b\\ more")
	f.Add("\x1b_Ga=p,i=7,x=1,y=2,w=3,h=4\x1b\\z")
	RegisterKittyImageMetadata(KittyImageMetadata{ImageID: 7, Columns: 4, Rows: 3, WidthPx: 100, HeightPx: 60})
	f.Fuzz(func(t *testing.T, line string) {
		GetKittyImageMetadata(line)
		GetKittyImagePlacementRows(line)
		GetKittyImagePlacement(line)
		CropKittyImageLine(line, 1, 2)
		CropKittyImageLine(line, 0, 10)
	})
}
