package tui

// Port of pi v1.3.0 packages/tui/src/terminal-image.ts: inline-image capability
// detection inputs, the kitty and iTerm2 encoders, image dimension parsing,
// cell-size math, the kitty placement registry used for scroll clipping, and
// the text fallback. The package-level capability value is injected (D69), so
// detection itself lives with the caller; this file only consumes it.

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Inline-image protocol names for TerminalCapabilities.Images.
const (
	ImageProtocolKitty  = "kitty"
	ImageProtocolITerm2 = "iterm2"
)

// CellDimensions is the terminal cell size in pixels.
type CellDimensions struct {
	WidthPx  int
	HeightPx int
}

// ImageDimensions is an image's pixel size.
type ImageDimensions struct {
	WidthPx  int
	HeightPx int
}

// ImageCellSize is a placed image's size in terminal cells.
type ImageCellSize struct {
	Columns int
	Rows    int
}

var cellDimensionsState struct {
	mu   sync.RWMutex
	dims CellDimensions
}

func init() {
	cellDimensionsState.dims = CellDimensions{WidthPx: 9, HeightPx: 18}
}

// GetCellDimensions returns the terminal cell size (default 9x18).
func GetCellDimensions() CellDimensions {
	cellDimensionsState.mu.RLock()
	defer cellDimensionsState.mu.RUnlock()
	return cellDimensionsState.dims
}

// SetCellDimensions installs the terminal cell size.
func SetCellDimensions(dims CellDimensions) {
	cellDimensionsState.mu.Lock()
	cellDimensionsState.dims = dims
	cellDimensionsState.mu.Unlock()
}

// AllocateImageID returns a random kitty image id in [1, 0xfffffffe].
func AllocateImageID() int {
	return int(rand.Uint32()%0xfffffffe) + 1
}

// KittyEncodeOptions are the optional parameters of EncodeKitty.
type KittyEncodeOptions struct {
	Columns    int
	HasColumns bool
	Rows       int
	HasRows    bool
	ImageID    int
	HasImageID bool
	// MoveCursor, when non-nil false, emits C=1 (no terminal-side movement).
	MoveCursor *bool
}

// EncodeKitty builds the kitty graphics transmission for base64 PNG data.
func EncodeKitty(base64Data string, options KittyEncodeOptions) string {
	const chunkSize = 4096
	params := []string{"a=T", "f=100", "q=2"}
	if options.MoveCursor != nil && !*options.MoveCursor {
		params = append(params, "C=1")
	}
	if options.HasColumns {
		params = append(params, "c="+strconv.Itoa(options.Columns))
	}
	if options.HasRows {
		params = append(params, "r="+strconv.Itoa(options.Rows))
	}
	if options.HasImageID {
		params = append(params, "i="+strconv.Itoa(options.ImageID))
	}
	joined := strings.Join(params, ",")
	if len(base64Data) <= chunkSize {
		return "\x1b_G" + joined + ";" + base64Data + "\x1b\\"
	}
	var chunks []string
	offset := 0
	isFirst := true
	for offset < len(base64Data) {
		end := offset + chunkSize
		if end > len(base64Data) {
			end = len(base64Data)
		}
		chunk := base64Data[offset:end]
		isLast := offset+chunkSize >= len(base64Data)
		switch {
		case isFirst:
			chunks = append(chunks, "\x1b_G"+joined+",m=1;"+chunk+"\x1b\\")
			isFirst = false
		case isLast:
			chunks = append(chunks, "\x1b_Gm=0;"+chunk+"\x1b\\")
		default:
			chunks = append(chunks, "\x1b_Gm=1;"+chunk+"\x1b\\")
		}
		offset += chunkSize
	}
	return strings.Join(chunks, "")
}

// Iterm2EncodeOptions are the optional parameters of EncodeITerm2.
type Iterm2EncodeOptions struct {
	// Width and Height are emitted verbatim; Width is a cell count and Height
	// is usually "auto".
	Width    string
	HasWidth bool
	Height   string
	// Name, when set, is base64-encoded into the name parameter.
	Name    string
	HasName bool
	// PreserveAspectRatio, when non-nil false, emits preserveAspectRatio=0.
	PreserveAspectRatio *bool
	// Inline, when non-nil false, emits inline=0.
	Inline *bool
}

// EncodeITerm2 builds an iTerm2 OSC 1337 inline-image sequence.
func EncodeITerm2(base64Data string, options Iterm2EncodeOptions) string {
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		decoded = nil
	}
	inline := "1"
	if options.Inline != nil && !*options.Inline {
		inline = "0"
	}
	params := []string{"inline=" + inline, "size=" + strconv.Itoa(len(decoded))}
	if options.HasWidth {
		params = append(params, "width="+options.Width)
	}
	if options.Height != "" {
		params = append(params, "height="+options.Height)
	}
	if options.HasName {
		params = append(params, "name="+base64.StdEncoding.EncodeToString([]byte(options.Name)))
	}
	if options.PreserveAspectRatio != nil && !*options.PreserveAspectRatio {
		params = append(params, "preserveAspectRatio=0")
	}
	return "\x1b]1337;File=" + strings.Join(params, ";") + ":" + base64Data + "\x07"
}

var (
	kittyControlsPattern = regexp.MustCompile("\x1b_G([^;]*)")
	kittyImageIDPattern  = regexp.MustCompile(`(?:^|,)i=(\d+)(?:,|$)`)
	kittyRowsPattern     = regexp.MustCompile(`(?:^|,)r=(\d+)(?:,|$)`)
	kittyMorePattern     = regexp.MustCompile(`(?:^|,)m=1(?:,|$)`)
	kittyCropDropPattern = regexp.MustCompile(`^[yhr]=`)
)

// RegisteredKittyImage is one placed image's metadata plus its transmission
// generation.
type RegisteredKittyImage struct {
	KittyImageMetadata
	TransmissionGeneration int
}

// KittyImagePlacement is the placement-only command for an image line.
type KittyImagePlacement struct {
	ImageID                int
	TransmissionGeneration int
	TransmissionBytes      int
	EstimatedDecodedBytes  int
	Rows                   int
	Sequence               string
	ReplacementLine        string
}

var kittyPlacementControlKeys = map[string]bool{
	"i": true, "p": true, "x": true, "y": true, "w": true, "h": true,
	"X": true, "Y": true, "c": true, "r": true, "C": true, "U": true,
	"z": true, "P": true, "Q": true, "H": true, "V": true,
}

var kittyRegistry struct {
	mu         sync.Mutex
	generation int
	order      []int
	byID       map[int]RegisteredKittyImage
}

// RegisterKittyImageMetadata records a placed image so later crop/placement
// lookups can reconstruct its controls.
func RegisterKittyImageMetadata(metadata KittyImageMetadata) {
	kittyRegistry.mu.Lock()
	defer kittyRegistry.mu.Unlock()
	if kittyRegistry.byID == nil {
		kittyRegistry.byID = map[int]RegisteredKittyImage{}
	}
	kittyRegistry.generation++
	kittyRegistry.byID[metadata.ImageID] = RegisteredKittyImage{
		KittyImageMetadata:     metadata,
		TransmissionGeneration: kittyRegistry.generation,
	}
	filtered := kittyRegistry.order[:0]
	for _, id := range kittyRegistry.order {
		if id != metadata.ImageID {
			filtered = append(filtered, id)
		}
	}
	kittyRegistry.order = append(filtered, metadata.ImageID)
	if len(kittyRegistry.order) > 1000 {
		oldest := kittyRegistry.order[0]
		kittyRegistry.order = kittyRegistry.order[1:]
		delete(kittyRegistry.byID, oldest)
	}
}

func registeredKittyFromControls(controls string) (RegisteredKittyImage, bool) {
	match := kittyImageIDPattern.FindStringSubmatch(controls)
	if match == nil {
		return RegisteredKittyImage{}, false
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return RegisteredKittyImage{}, false
	}
	kittyRegistry.mu.Lock()
	defer kittyRegistry.mu.Unlock()
	metadata, ok := kittyRegistry.byID[id]
	return metadata, ok
}

func registeredKittyFromLine(line string) (RegisteredKittyImage, bool) {
	match := kittyControlsPattern.FindStringSubmatch(line)
	if match == nil {
		return RegisteredKittyImage{}, false
	}
	return registeredKittyFromControls(match[1])
}

// GetKittyImageMetadata reports the registered metadata for an image line.
func GetKittyImageMetadata(line string) (KittyImageMetadata, bool) {
	metadata, ok := registeredKittyFromLine(line)
	if !ok {
		return KittyImageMetadata{}, false
	}
	return metadata.KittyImageMetadata, true
}

func explicitKittyRows(controls string) (int, bool) {
	match := kittyRowsPattern.FindStringSubmatch(controls)
	if match == nil {
		return 0, false
	}
	rows, err := strconv.Atoi(match[1])
	if err != nil || rows <= 0 {
		return 0, false
	}
	return rows, true
}

// GetKittyImagePlacementRows reports the row count a placement covers without
// scanning its payload.
func GetKittyImagePlacementRows(line string) (int, bool) {
	match := kittyControlsPattern.FindStringSubmatch(line)
	if match == nil {
		return 0, false
	}
	if rows, ok := explicitKittyRows(match[1]); ok {
		return rows, true
	}
	metadata, ok := registeredKittyFromControls(match[1])
	if !ok {
		return 0, false
	}
	return metadata.Rows, true
}

// GetKittyImagePlacement builds a placement-only command for an image line
// emitted by RenderImage.
func GetKittyImagePlacement(line string) (KittyImagePlacement, bool) {
	match := kittyControlsPattern.FindStringSubmatchIndex(line)
	if match == nil {
		return KittyImagePlacement{}, false
	}
	controls := line[match[2]:match[3]]
	metadata, ok := registeredKittyFromControls(controls)
	if !ok {
		return KittyImagePlacement{}, false
	}

	commandStart := match[0]
	commandControls := controls
	transmissionEnd := 0
	for {
		relative := strings.Index(line[commandStart+len(kittyPrefix):], "\x1b\\")
		if relative == -1 {
			return KittyImagePlacement{}, false
		}
		transmissionEnd = commandStart + len(kittyPrefix) + relative + 2
		if !kittyMorePattern.MatchString(commandControls) {
			break
		}
		commandStart = transmissionEnd
		if !strings.HasPrefix(line[commandStart:], kittyPrefix) {
			return KittyImagePlacement{}, false
		}
		controlsEnd := strings.IndexByte(line[commandStart+len(kittyPrefix):], ';')
		if controlsEnd == -1 {
			return KittyImagePlacement{}, false
		}
		controlsEnd += commandStart + len(kittyPrefix)
		commandControls = line[commandStart+len(kittyPrefix) : controlsEnd]
	}

	var kept []string
	for _, control := range strings.Split(controls, ",") {
		key, _, _ := strings.Cut(control, "=")
		if kittyPlacementControlKeys[key] {
			kept = append(kept, control)
		}
	}
	sequence := "\x1b_Ga=p,q=2," + strings.Join(kept, ",") + "\x1b\\"
	rows := metadata.Rows
	if explicit, ok := explicitKittyRows(controls); ok {
		rows = explicit
	}
	return KittyImagePlacement{
		ImageID:                metadata.ImageID,
		TransmissionGeneration: metadata.TransmissionGeneration,
		TransmissionBytes:      transmissionEnd - match[0],
		EstimatedDecodedBytes:  metadata.WidthPx * metadata.HeightPx * 4,
		Rows:                   rows,
		Sequence:               sequence,
		ReplacementLine:        line[:match[0]] + sequence + line[transmissionEnd:],
	}, true
}

// CropKittyImageLine crops a placed image to a visible row window, returning
// the line unchanged when no crop is needed.
func CropKittyImageLine(line string, hiddenRows int, visibleRows int) string {
	metadata, ok := GetKittyImageMetadata(line)
	match := kittyControlsPattern.FindStringSubmatchIndex(line)
	if !ok || match == nil || hiddenRows < 0 || hiddenRows >= metadata.Rows || visibleRows <= 0 {
		return line
	}
	croppedRows := visibleRows
	if metadata.Rows-hiddenRows < croppedRows {
		croppedRows = metadata.Rows - hiddenRows
	}
	if hiddenRows == 0 && croppedRows == metadata.Rows {
		return line
	}
	sourceY := int(math.Floor(float64(metadata.HeightPx*hiddenRows) / float64(metadata.Rows)))
	sourceEnd := int(math.Ceil(float64(metadata.HeightPx*(hiddenRows+croppedRows)) / float64(metadata.Rows)))
	sourceHeight := metadata.HeightPx
	if sourceEnd < sourceHeight {
		sourceHeight = sourceEnd
	}
	sourceHeight -= sourceY
	if sourceHeight < 1 {
		sourceHeight = 1
	}
	var controls []string
	for _, control := range strings.Split(line[match[2]:match[3]], ",") {
		if !kittyCropDropPattern.MatchString(control) {
			controls = append(controls, control)
		}
	}
	controls = append(controls, "y="+strconv.Itoa(sourceY), "h="+strconv.Itoa(sourceHeight), "r="+strconv.Itoa(croppedRows))
	return line[:match[0]] + "\x1b_G" + strings.Join(controls, ",") + ";" + line[match[1]:]
}

func chooseLessDistortedCellCount(upperCount int, idealCount float64) int {
	if upperCount <= 1 {
		return upperCount
	}
	lowerCount := upperCount - 1
	upperDistortion := math.Max(float64(upperCount)/idealCount, idealCount/float64(upperCount))
	lowerDistortion := math.Max(float64(lowerCount)/idealCount, idealCount/float64(lowerCount))
	if lowerDistortion < upperDistortion {
		return lowerCount
	}
	return upperCount
}

// CalculateImageCellSize converts pixel dimensions to terminal cells.
func CalculateImageCellSize(imageDimensions ImageDimensions, maxWidthCells int, maxHeightCells *int, cellDimensions CellDimensions, optimizeAspectRatio bool) ImageCellSize {
	maxWidth := maxWidthCells
	if maxWidth < 1 {
		maxWidth = 1
	}
	var maxHeight int
	if maxHeightCells != nil {
		maxHeight = *maxHeightCells
		if maxHeight < 1 {
			maxHeight = 1
		}
	}
	imageWidth := imageDimensions.WidthPx
	if imageWidth < 1 {
		imageWidth = 1
	}
	imageHeight := imageDimensions.HeightPx
	if imageHeight < 1 {
		imageHeight = 1
	}
	widthScale := float64(maxWidth*cellDimensions.WidthPx) / float64(imageWidth)
	heightScale := widthScale
	if maxHeightCells != nil {
		heightScale = float64(maxHeight*cellDimensions.HeightPx) / float64(imageHeight)
	}
	scale := math.Min(widthScale, heightScale)
	scaledWidthPx := float64(imageWidth) * scale
	scaledHeightPx := float64(imageHeight) * scale
	columns := int(math.Ceil(scaledWidthPx / float64(cellDimensions.WidthPx)))
	if columns < 1 {
		columns = 1
	}
	if columns > maxWidth {
		columns = maxWidth
	}
	heightRows := scaledHeightPx / float64(cellDimensions.HeightPx)
	rows := int(math.Ceil(heightRows))
	if rows < 1 {
		rows = 1
	}
	if maxHeightCells != nil && rows > maxHeight {
		rows = maxHeight
	}
	if !optimizeAspectRatio {
		return ImageCellSize{Columns: columns, Rows: rows}
	}
	if widthScale <= heightScale {
		idealRows := (float64(columns) * float64(cellDimensions.WidthPx) * float64(imageHeight)) / (float64(imageWidth) * float64(cellDimensions.HeightPx))
		rows = chooseLessDistortedCellCount(rows, idealRows)
	} else {
		idealColumns := (float64(rows) * float64(cellDimensions.HeightPx) * float64(imageWidth)) / (float64(imageHeight) * float64(cellDimensions.WidthPx))
		columns = chooseLessDistortedCellCount(columns, idealColumns)
	}
	return ImageCellSize{Columns: columns, Rows: rows}
}

// CalculateImageRows is the row count for a target width.
func CalculateImageRows(imageDimensions ImageDimensions, targetWidthCells int, cellDimensions CellDimensions) int {
	return CalculateImageCellSize(imageDimensions, targetWidthCells, nil, cellDimensions, false).Rows
}

func decodeBase64(data string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil
	}
	return decoded
}

// GetPngDimensions parses a PNG's IHDR dimensions.
func GetPngDimensions(base64Data string) (ImageDimensions, bool) {
	buffer := decodeBase64(base64Data)
	if len(buffer) < 24 {
		return ImageDimensions{}, false
	}
	if buffer[0] != 0x89 || buffer[1] != 0x50 || buffer[2] != 0x4e || buffer[3] != 0x47 {
		return ImageDimensions{}, false
	}
	return ImageDimensions{WidthPx: int(binary.BigEndian.Uint32(buffer[16:])), HeightPx: int(binary.BigEndian.Uint32(buffer[20:]))}, true
}

// GetJpegDimensions parses a JPEG SOF marker.
func GetJpegDimensions(base64Data string) (ImageDimensions, bool) {
	buffer := decodeBase64(base64Data)
	if len(buffer) < 2 || buffer[0] != 0xff || buffer[1] != 0xd8 {
		return ImageDimensions{}, false
	}
	offset := 2
	for offset < len(buffer)-9 {
		if buffer[offset] != 0xff {
			offset++
			continue
		}
		marker := buffer[offset+1]
		if marker >= 0xc0 && marker <= 0xc2 {
			return ImageDimensions{
				WidthPx:  int(binary.BigEndian.Uint16(buffer[offset+7:])),
				HeightPx: int(binary.BigEndian.Uint16(buffer[offset+5:])),
			}, true
		}
		if offset+3 >= len(buffer) {
			return ImageDimensions{}, false
		}
		length := int(binary.BigEndian.Uint16(buffer[offset+2:]))
		if length < 2 {
			return ImageDimensions{}, false
		}
		offset += 2 + length
	}
	return ImageDimensions{}, false
}

// GetGifDimensions parses a GIF logical screen descriptor.
func GetGifDimensions(base64Data string) (ImageDimensions, bool) {
	buffer := decodeBase64(base64Data)
	if len(buffer) < 10 {
		return ImageDimensions{}, false
	}
	signature := string(buffer[:6])
	if signature != "GIF87a" && signature != "GIF89a" {
		return ImageDimensions{}, false
	}
	return ImageDimensions{
		WidthPx:  int(binary.LittleEndian.Uint16(buffer[6:])),
		HeightPx: int(binary.LittleEndian.Uint16(buffer[8:])),
	}, true
}

// GetWebpDimensions parses a WebP VP8/VP8L/VP8X header.
func GetWebpDimensions(base64Data string) (ImageDimensions, bool) {
	buffer := decodeBase64(base64Data)
	if len(buffer) < 30 {
		return ImageDimensions{}, false
	}
	if string(buffer[:4]) != "RIFF" || string(buffer[8:12]) != "WEBP" {
		return ImageDimensions{}, false
	}
	switch string(buffer[12:16]) {
	case "VP8 ":
		return ImageDimensions{
			WidthPx:  int(binary.LittleEndian.Uint16(buffer[26:]) & 0x3fff),
			HeightPx: int(binary.LittleEndian.Uint16(buffer[28:]) & 0x3fff),
		}, true
	case "VP8L":
		if len(buffer) < 25 {
			return ImageDimensions{}, false
		}
		bits := binary.LittleEndian.Uint32(buffer[21:])
		return ImageDimensions{WidthPx: int(bits&0x3fff) + 1, HeightPx: int((bits>>14)&0x3fff) + 1}, true
	case "VP8X":
		width := int(buffer[24]) | int(buffer[25])<<8 | int(buffer[26])<<16
		height := int(buffer[27]) | int(buffer[28])<<8 | int(buffer[29])<<16
		return ImageDimensions{WidthPx: width + 1, HeightPx: height + 1}, true
	}
	return ImageDimensions{}, false
}

// GetImageDimensions dispatches to the format parser for a MIME type.
func GetImageDimensions(base64Data string, mimeType string) (ImageDimensions, bool) {
	switch mimeType {
	case "image/png":
		return GetPngDimensions(base64Data)
	case "image/jpeg":
		return GetJpegDimensions(base64Data)
	case "image/gif":
		return GetGifDimensions(base64Data)
	case "image/webp":
		return GetWebpDimensions(base64Data)
	}
	return ImageDimensions{}, false
}

// ImageRenderOptions are the optional parameters of RenderImage.
type ImageRenderOptions struct {
	MaxWidthCells       int
	MaxHeightCells      *int
	PreserveAspectRatio *bool
	ImageID             int
	HasImageID          bool
	MoveCursor          *bool
}

// RenderedImage is RenderImage's outcome.
type RenderedImage struct {
	Sequence   string
	Columns    int
	Rows       int
	ImageID    int
	HasImageID bool
}

// RenderImage encodes an image for the active inline-image protocol.
func RenderImage(base64Data string, imageDimensions ImageDimensions, options ImageRenderOptions) (RenderedImage, bool) {
	caps := GetTerminalCapabilities()
	if caps.Images == "" {
		return RenderedImage{}, false
	}
	maxWidth := options.MaxWidthCells
	if maxWidth == 0 {
		maxWidth = 80
	}
	size := CalculateImageCellSize(imageDimensions, maxWidth, options.MaxHeightCells, GetCellDimensions(), caps.Images == ImageProtocolKitty)
	if caps.Images == ImageProtocolKitty {
		if options.HasImageID {
			RegisterKittyImageMetadata(KittyImageMetadata{
				ImageID: options.ImageID, Columns: size.Columns, Rows: size.Rows,
				WidthPx: imageDimensions.WidthPx, HeightPx: imageDimensions.HeightPx,
			})
		}
		sequence := EncodeKitty(base64Data, KittyEncodeOptions{
			Columns: size.Columns, HasColumns: true, Rows: size.Rows, HasRows: true,
			ImageID: options.ImageID, HasImageID: options.HasImageID, MoveCursor: options.MoveCursor,
		})
		return RenderedImage{Sequence: sequence, Columns: size.Columns, Rows: size.Rows, ImageID: options.ImageID, HasImageID: options.HasImageID}, true
	}
	if caps.Images == ImageProtocolITerm2 {
		preserve := true
		if options.PreserveAspectRatio != nil {
			preserve = *options.PreserveAspectRatio
		}
		sequence := EncodeITerm2(base64Data, Iterm2EncodeOptions{
			Width: strconv.Itoa(size.Columns), HasWidth: true, Height: "auto",
			PreserveAspectRatio: &preserve,
		})
		return RenderedImage{Sequence: sequence, Columns: size.Columns, Rows: size.Rows}, true
	}
	return RenderedImage{}, false
}

// Hyperlink wraps text in OSC 8 open/close sequences (defined in markdown.go).

var cellSizeResponsePattern = regexp.MustCompile(`^\x1b\[6;(\d+);(\d+)t$`)

// ParseCellSizeResponse parses a CSI 6 ; height ; width t reply (the reply to
// the CSI 16 t cell-size query).
func ParseCellSizeResponse(data string) (CellDimensions, bool) {
	match := cellSizeResponsePattern.FindStringSubmatch(data)
	if match == nil {
		return CellDimensions{}, false
	}
	heightPx, err := strconv.Atoi(match[1])
	if err != nil {
		return CellDimensions{}, false
	}
	widthPx, err := strconv.Atoi(match[2])
	if err != nil {
		return CellDimensions{}, false
	}
	return CellDimensions{WidthPx: widthPx, HeightPx: heightPx}, true
}

func shortenImagePath(filename string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filename
	}
	if filename == home || strings.HasPrefix(filename, home+"/") || strings.HasPrefix(filename, home+"\\") {
		return "~" + filename[len(home):]
	}
	return filename
}

func pathToFileURL(path string) string {
	trimmed := strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return "file://" + (&url.URL{Path: trimmed}).EscapedPath()
}

// ImageFallback is the text shown when a terminal cannot render the image.
func ImageFallback(mimeType string, dimensions *ImageDimensions, filename string) string {
	var parts []string
	if filename != "" {
		display := shortenImagePath(filename)
		if GetTerminalCapabilities().Hyperlinks && strings.HasPrefix(filename, "/") {
			parts = append(parts, Hyperlink(display, pathToFileURL(filename)))
		} else {
			parts = append(parts, display)
		}
	}
	parts = append(parts, "["+mimeType+"]")
	if dimensions != nil {
		parts = append(parts, strconv.Itoa(dimensions.WidthPx)+"x"+strconv.Itoa(dimensions.HeightPx))
	}
	return "[Image: " + strings.Join(parts, " ") + "]"
}
