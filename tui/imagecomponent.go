package tui

// Port of pi v1.4.0 packages/tui/src/components/image.ts: the inline-image
// component. It renders the active protocol's sequence with padding rows for
// the reserved height, or the text fallback when the terminal cannot render
// images.

import (
	"strconv"
	"sync"
)

// ImageTranscoder converts non-PNG base64 data to base64 PNG for the kitty
// protocol, which only accepts PNG. A nil result means conversion failed.
type ImageTranscoder func(base64Data string, mimeType string) (string, bool)

// ImageTheme styles the fallback text.
type ImageTheme struct {
	// FallbackColor styles the fallback line; nil is identity.
	FallbackColor func(string) string
}

// ImageOptions configure an ImageComponent.
type ImageOptions struct {
	MaxWidthCells  *int
	MaxHeightCells *int
	Filename       string
	// ImageID reuses a kitty image id when set (animations/updates).
	ImageID *int
}

// ImageComponent renders one inline image.
type ImageComponent struct {
	base64Data string
	mimeType   string
	theme      ImageTheme
	options    ImageOptions
	dimensions ImageDimensions

	imageID int
	hasID   bool
	pngData *string

	cachedLines []string
	cachedWidth int
	hasCache    bool
}

// NewImageComponent builds an image component. When dimensions is nil the
// pixel size is parsed from the data (falling back to 800x600).
func NewImageComponent(base64Data string, mimeType string, theme ImageTheme, options ImageOptions, dimensions *ImageDimensions) *ImageComponent {
	component := &ImageComponent{
		base64Data: base64Data, mimeType: mimeType, theme: theme, options: options,
	}
	if dimensions != nil {
		component.dimensions = *dimensions
	} else if parsed, ok := GetImageDimensions(base64Data, mimeType); ok {
		component.dimensions = parsed
	} else {
		component.dimensions = ImageDimensions{WidthPx: 800, HeightPx: 600}
	}
	if options.ImageID != nil {
		component.imageID = *options.ImageID
		component.hasID = true
	}
	return component
}

// GetImageID reports the kitty image id in use.
func (c *ImageComponent) GetImageID() (int, bool) {
	return c.imageID, c.hasID
}

// Invalidate drops the cached render.
func (c *ImageComponent) Invalidate() {
	c.cachedLines = nil
	c.cachedWidth = 0
	c.hasCache = false
}

// Render renders the image for the viewport width.
func (c *ImageComponent) Render(width int) []string {
	if c.hasCache && c.cachedWidth == width {
		return c.cachedLines
	}
	maxWidth := width - 2
	if configured := c.options.MaxWidthCells; configured != nil && *configured < maxWidth {
		maxWidth = *configured
	}
	if maxWidth < 1 {
		maxWidth = 1
	}
	cell := GetCellDimensions()
	defaultMaxHeight := max(int((maxWidth*cell.WidthPx+cell.HeightPx-1)/cell.HeightPx), 1)
	maxHeight := defaultMaxHeight
	if configured := c.options.MaxHeightCells; configured != nil {
		maxHeight = *configured
	}

	caps := GetTerminalCapabilities()
	data := c.base64Data
	dimensions := c.dimensions
	if caps.Images == ImageProtocolKitty && c.mimeType != "image/png" {
		if c.pngData == nil {
			if png, ok := transcodeToPng(c.base64Data, c.mimeType); ok {
				c.pngData = &png
			}
			// A failed conversion is not stored, so a later transcoder can
			// retry; data stays empty and the fallback renders.
		}
		if c.pngData != nil {
			data = *c.pngData
			if parsed, ok := GetPngDimensions(data); ok {
				dimensions = parsed
			}
		} else {
			data = ""
		}
	}

	var lines []string
	if caps.Images != "" && data != "" {
		moveCursor := false
		options := ImageRenderOptions{MaxWidthCells: maxWidth, MaxHeightCells: &maxHeight, ImageID: c.imageID, HasImageID: c.hasID, MoveCursor: &moveCursor}
		if caps.Images == ImageProtocolKitty && !c.hasID {
			c.imageID = AllocateImageID()
			c.hasID = true
			options.ImageID = c.imageID
			options.HasImageID = true
		}
		result, ok := RenderImage(data, dimensions, options)
		if ok {
			if result.HasImageID {
				c.imageID = result.ImageID
				c.hasID = true
			}
			if caps.Images == ImageProtocolKitty {
				// C=1 prevents cursor movement; reserve `rows` lines.
				lines = make([]string, result.Rows)
				lines[0] = result.Sequence
			} else {
				// The first rows-1 lines are cleared before the image is
				// drawn; the last line moves up, draws, then leaves the cursor
				// inside the scroll area.
				lines = make([]string, result.Rows)
				rowOffset := result.Rows - 1
				moveUp := ""
				if rowOffset > 0 {
					moveUp = "\x1b[" + strconv.Itoa(rowOffset) + "A"
				}
				lines[rowOffset] = moveUp + result.Sequence
			}
		} else {
			lines = []string{c.fallbackLine(width)}
		}
	} else {
		lines = []string{c.fallbackLine(width)}
	}

	c.cachedLines = lines
	c.cachedWidth = width
	c.hasCache = true
	return lines
}

func (c *ImageComponent) fallbackLine(width int) string {
	fallback := ImageFallback(c.mimeType, &c.dimensions, c.options.Filename)
	if c.theme.FallbackColor != nil {
		fallback = c.theme.FallbackColor(fallback)
	}
	return TruncateToWidth(fallback, width, "...", false)
}

// ---- PNG transcoding ----

const imageTranscodeCacheLimit = 32

var imageTranscodeState struct {
	mu         sync.Mutex
	transcoder ImageTranscoder
	cache      map[string]*string
	order      []string
}

// SetImageTranscoder installs the non-PNG to PNG converter used by the kitty
// protocol. Passing nil clears both the transcoder and its cache.
func SetImageTranscoder(transcoder ImageTranscoder) {
	imageTranscodeState.mu.Lock()
	defer imageTranscodeState.mu.Unlock()
	imageTranscodeState.transcoder = transcoder
	imageTranscodeState.cache = nil
	imageTranscodeState.order = nil
}

func transcodeToPng(base64Data string, mimeType string) (string, bool) {
	imageTranscodeState.mu.Lock()
	defer imageTranscodeState.mu.Unlock()
	transcoder := imageTranscodeState.transcoder
	if transcoder == nil {
		return "", false
	}
	if cached, ok := imageTranscodeState.cache[base64Data]; ok {
		if cached == nil {
			return "", false
		}
		return *cached, true
	}
	png, ok := transcoder(base64Data, mimeType)
	if !ok {
		if imageTranscodeState.cache == nil {
			imageTranscodeState.cache = map[string]*string{}
		}
		imageTranscodeState.cache[base64Data] = nil
		return "", false
	}
	if imageTranscodeState.cache == nil {
		imageTranscodeState.cache = map[string]*string{}
	}
	value := png
	imageTranscodeState.cache[base64Data] = &value
	imageTranscodeState.order = append(imageTranscodeState.order, base64Data)
	if len(imageTranscodeState.order) > imageTranscodeCacheLimit {
		oldest := imageTranscodeState.order[0]
		imageTranscodeState.order = imageTranscodeState.order[1:]
		delete(imageTranscodeState.cache, oldest)
	}
	return png, true
}
