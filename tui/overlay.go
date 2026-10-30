package tui

import (
	"math"
	"strconv"
	"strings"
)

// Port of the overlay layout half of src/tui.ts: anchor/percentage/absolute
// positioning with margins, and line compositing.

// OverlayAnchor is the anchor position for overlays.
type OverlayAnchor = string

const (
	OverlayAnchorCenter       OverlayAnchor = "center"
	OverlayAnchorTopLeft      OverlayAnchor = "top-left"
	OverlayAnchorTopRight     OverlayAnchor = "top-right"
	OverlayAnchorBottomLeft   OverlayAnchor = "bottom-left"
	OverlayAnchorBottomRight  OverlayAnchor = "bottom-right"
	OverlayAnchorTopCenter    OverlayAnchor = "top-center"
	OverlayAnchorBottomCenter OverlayAnchor = "bottom-center"
	OverlayAnchorLeftCenter   OverlayAnchor = "left-center"
	OverlayAnchorRightCenter  OverlayAnchor = "right-center"
)

// OverlayMargin is the margin from terminal edges.
type OverlayMargin struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

// OverlayOptions configure overlay positioning and sizing. Size values can be
// absolute (int) or percentage (Percent).
type OverlayOptions struct {
	// Width in columns, or percentage of terminal width.
	Width        *int
	PercentWidth string
	MinWidth     *int
	// MaxHeight in rows, or percentage of terminal height.
	MaxHeight        *int
	PercentMaxHeight string
	// Anchor point for positioning (default center).
	Anchor OverlayAnchor
	// Offsets from the anchor position (positive = right/down).
	OffsetX int
	OffsetY int
	// Row position: absolute number, or percentage string (e.g. "25%").
	Row        *int
	RowPercent string
	// Column position: absolute number, or percentage string.
	Col        *int
	ColPercent string
	// Margin from terminal edges.
	Margin OverlayMargin
	// Visible controls overlay visibility from the terminal dimensions.
	Visible func(termWidth int, termHeight int) bool
	// NonCapturing suppresses focus capture when the overlay is shown.
	NonCapturing bool
}

// OverlayBounds is the last rendered terminal-relative overlay rectangle.
type OverlayBounds struct {
	Row    int
	Col    int
	Width  int
	Height int
}

// parsePercentValue parses "NN%" strings; ok is false otherwise.
func parsePercentValue(value string) (float64, bool) {
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	number := strings.TrimSuffix(value, "%")
	if number == "" {
		return 0, false
	}
	parts := strings.Split(number, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return 0, false
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
	}
	percent, _ := strconv.ParseFloat(number, 64)
	return percent, true
}

func percentOf(reference int, percent float64) int {
	return floatToIntSaturated(math.Floor(float64(reference) * percent / 100))
}

func percentPosition(base int, reference int, percent float64) int {
	value := float64(base) + math.Floor(float64(reference)*percent/100)
	return floatToIntSaturated(value)
}

func floatToIntSaturated(value float64) int {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if value >= float64(maxInt) {
		return maxInt
	}
	if value <= float64(minInt) {
		return minInt
	}
	return int(value)
}

func addIntSaturated(a int, b int) int {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if b > 0 && a > maxInt-b {
		return maxInt
	}
	if b < 0 && a < minInt-b {
		return minInt
	}
	return a + b
}

// resolveOverlayLayout resolves the overlay width and position from options.
type overlayLayout struct {
	Width     int
	Row       int
	Col       int
	MaxHeight int // -1 when unset
	HasMax    bool
}

func resolveOverlayLayout(options *OverlayOptions, overlayHeight int, termWidth int, termHeight int) overlayLayout {
	opt := OverlayOptions{}
	if options != nil {
		opt = *options
	}

	marginTop := max(0, opt.Margin.Top)
	marginRight := max(0, opt.Margin.Right)
	marginBottom := max(0, opt.Margin.Bottom)
	marginLeft := max(0, opt.Margin.Left)

	availWidth := max(1, termWidth-marginLeft-marginRight)
	availHeight := max(1, termHeight-marginTop-marginBottom)

	// Width: percentage of the terminal, else min(80, available).
	layoutWidth := 0
	hasWidth := false
	if opt.Width != nil {
		layoutWidth = *opt.Width
		hasWidth = true
	} else if opt.PercentWidth != "" {
		if percent, ok := parsePercentValue(opt.PercentWidth); ok {
			layoutWidth = percentOf(termWidth, percent)
			hasWidth = true
		}
	}
	if !hasWidth {
		layoutWidth = min(80, availWidth)
	}
	if opt.MinWidth != nil {
		layoutWidth = max(layoutWidth, *opt.MinWidth)
	}
	layoutWidth = max(1, min(layoutWidth, availWidth))

	// Max height: percentage of the terminal, clamped to available space.
	maxHeight := 0
	hasMax := false
	if opt.MaxHeight != nil {
		maxHeight = *opt.MaxHeight
		hasMax = true
	} else if opt.PercentMaxHeight != "" {
		if percent, ok := parsePercentValue(opt.PercentMaxHeight); ok {
			maxHeight = percentOf(termHeight, percent)
			hasMax = true
		}
	}
	if hasMax {
		maxHeight = max(1, min(maxHeight, availHeight))
	}

	effectiveHeight := overlayHeight
	if hasMax {
		effectiveHeight = min(overlayHeight, maxHeight)
	}

	// Row: explicit, percentage, or anchor-based.
	row := 0
	switch {
	case opt.Row != nil:
		row = *opt.Row
	case opt.RowPercent != "":
		if percent, ok := parsePercentValue(opt.RowPercent); ok {
			maxRow := max(0, availHeight-effectiveHeight)
			row = percentPosition(marginTop, maxRow, percent)
		} else {
			row = resolveAnchorRow(OverlayAnchorCenter, effectiveHeight, availHeight, marginTop)
		}
	default:
		anchor := opt.Anchor
		if anchor == "" {
			anchor = OverlayAnchorCenter
		}
		row = resolveAnchorRow(anchor, effectiveHeight, availHeight, marginTop)
	}

	col := 0
	switch {
	case opt.Col != nil:
		col = *opt.Col
	case opt.ColPercent != "":
		if percent, ok := parsePercentValue(opt.ColPercent); ok {
			maxCol := max(0, availWidth-layoutWidth)
			col = percentPosition(marginLeft, maxCol, percent)
		} else {
			col = resolveAnchorCol(OverlayAnchorCenter, layoutWidth, availWidth, marginLeft)
		}
	default:
		anchor := opt.Anchor
		if anchor == "" {
			anchor = OverlayAnchorCenter
		}
		col = resolveAnchorCol(anchor, layoutWidth, availWidth, marginLeft)
	}

	// Offsets, then clamping within the margins.
	row = addIntSaturated(row, opt.OffsetY)
	col = addIntSaturated(col, opt.OffsetX)
	row = max(marginTop, min(row, termHeight-marginBottom-effectiveHeight))
	col = max(marginLeft, min(col, termWidth-marginRight-layoutWidth))

	return overlayLayout{Width: layoutWidth, Row: row, Col: col, MaxHeight: maxHeight, HasMax: hasMax}
}

func resolveAnchorRow(anchor OverlayAnchor, height int, availHeight int, marginTop int) int {
	switch anchor {
	case OverlayAnchorTopLeft, OverlayAnchorTopCenter, OverlayAnchorTopRight:
		return marginTop
	case OverlayAnchorBottomLeft, OverlayAnchorBottomCenter, OverlayAnchorBottomRight:
		return marginTop + availHeight - height
	default: // center
		return marginTop + (availHeight-height)/2
	}
}

func resolveAnchorCol(anchor OverlayAnchor, width int, availWidth int, marginLeft int) int {
	switch anchor {
	case OverlayAnchorTopLeft, OverlayAnchorLeftCenter, OverlayAnchorBottomLeft:
		return marginLeft
	case OverlayAnchorTopRight, OverlayAnchorRightCenter, OverlayAnchorBottomRight:
		return marginLeft + availWidth - width
	default: // center
		return marginLeft + (availWidth-width)/2
	}
}

// segmentReset is the reset appended around composited overlay content.
const segmentReset = "\x1b[0m\x1b]8;;\x07"

// CompositeTuiLine composites overlay content into a terminal line at a fixed
// column. Image lines pass through untouched.
func CompositeTuiLine(baseLine string, overlayLine string, startCol int, overlayWidth int, totalWidth int) string {
	if isImageLine(baseLine) {
		return baseLine
	}

	afterStart := startCol + overlayWidth
	base := ExtractSegments(baseLine, startCol, afterStart, totalWidth-afterStart, true)
	overlay := SliceWithWidth(overlayLine, 0, overlayWidth, true)
	beforePad := max(0, startCol-base.BeforeWidth)
	overlayPad := max(0, overlayWidth-overlay.Width)
	actualBeforeWidth := max(startCol, base.BeforeWidth)
	actualOverlayWidth := max(overlayWidth, overlay.Width)
	afterTarget := max(0, totalWidth-actualBeforeWidth-actualOverlayWidth)
	afterPad := max(0, afterTarget-base.AfterWidth)

	var result strings.Builder
	result.WriteString(base.Before)
	result.WriteString(strings.Repeat(" ", beforePad))
	result.WriteString(segmentReset)
	result.WriteString(overlay.Text)
	result.WriteString(strings.Repeat(" ", overlayPad))
	result.WriteString(segmentReset)
	result.WriteString(base.After)
	result.WriteString(strings.Repeat(" ", afterPad))

	if VisibleWidth(result.String()) <= totalWidth {
		return result.String()
	}
	return SliceByColumn(result.String(), 0, totalWidth, true)
}

// minInt and maxInt mirror Math.min/Math.max's variadic form.
