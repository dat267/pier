package interactive

import (
	"fmt"
	"math"

	"github.com/dat267/pier/tui"
)

// Port of packages/coding-agent/src/modes/interactive/theme/system-theme.ts
// (upstream bf8e4b953): the `system` theme, pi's colors derived from the
// terminal's own reported foreground, background and palette.
//
// Every token belongs to a color family (its hue) and has contrast rules: it
// must reach a contrast level on the background and on the panels it is drawn
// on. Hue and saturation come from the terminal's palette color for the
// family's ANSI slot, or from the family's own hue when the terminal reports no
// palette. Lightness comes from the rules alone, in OKHSL.

// systemFamily is a family's OKHSL hue and saturation, plus the ANSI slot it
// takes its hue and saturation from.
type systemFamily struct {
	hue    float64
	satMin float64
	satMax float64
	slot   int
}

var systemFamilies = map[string]systemFamily{
	"neutral":            {231.49, 0.02, 0.08, 8},
	"blue":               {231.49, 0.1, 0.68, 4},
	"green":              {158.68, 0.1, 0.76, 2},
	"red":                {20, 0.1, 0.92, 1},
	"yellow":             {82.36, 0.5, 1, 3},
	"orange":             {52, 0.12, 0.85, 3},
	"violet":             {295, 0.2, 0.6, 5},
	"calamine":           {202.43, 0.1, 0.74, 6},
	"thinkingSlate":      {231.49, 0.08, 0.2, 4},
	"thinkingBlue":       {231.49, 0.2, 0.45, 4},
	"thinkingPeriwinkle": {263.25, 0.3, 0.6, 6},
	"thinkingViolet":     {295, 0.4, 0.75, 5},
	"thinkingMagenta":    {337.5, 0.5, 0.85, 13},
	"thinkingRed":        {20, 0.95, 1, 1},
}

// systemTokenOrder is the TOKEN_FAMILIES key order (the indexed tier walks it).
var systemTokenOrder = []string{
	"selectedBg", "searchMatchBg", "userMessageBg", "customMessageBg",
	"toolPendingBg", "toolSuccessBg", "toolErrorBg",
	"text", "userMessageText", "customMessageText", "toolTitle",
	"syntaxOperator", "syntaxPunctuation", "muted", "dim", "thinkingText",
	"toolOutput", "mdLinkUrl", "mdQuote", "mdQuoteBorder", "mdHr",
	"mdCodeBlockBorder", "toolDiffContext", "syntaxComment", "scrollbarTrack",
	"scrollbarThumb", "searchMatchText", "borderMuted",
	"accent", "borderAccent", "customMessageLabel", "mdCode", "mdListBullet",
	"syntaxType", "border", "mdLink", "syntaxKeyword", "syntaxVariable",
	"success", "mdCodeBlock", "toolDiffAdded", "bashMode", "syntaxNumber",
	"error", "toolDiffRemoved", "warning", "mdHeading", "syntaxFunction",
	"syntaxString",
	"thinkingOff", "thinkingMinimal", "thinkingLow", "thinkingMedium",
	"thinkingHigh", "thinkingXhigh", "thinkingMax",
}

var systemTokenFamilies = map[string]string{
	"selectedBg": "blue", "searchMatchBg": "orange", "userMessageBg": "blue",
	"customMessageBg": "violet", "toolPendingBg": "neutral",
	"toolSuccessBg": "green", "toolErrorBg": "red",
	"text": "neutral", "userMessageText": "neutral",
	"customMessageText": "neutral", "toolTitle": "neutral",
	"syntaxOperator": "neutral", "syntaxPunctuation": "neutral",
	"muted": "neutral", "dim": "neutral", "thinkingText": "neutral",
	"toolOutput": "neutral", "mdLinkUrl": "neutral", "mdQuote": "neutral",
	"mdQuoteBorder": "neutral", "mdHr": "neutral",
	"mdCodeBlockBorder": "neutral", "toolDiffContext": "neutral",
	"syntaxComment": "neutral", "scrollbarTrack": "neutral",
	"scrollbarThumb": "neutral", "searchMatchText": "neutral",
	"borderMuted": "neutral",
	"accent":      "violet", "borderAccent": "violet", "customMessageLabel": "violet",
	"mdCode": "violet", "mdListBullet": "violet", "syntaxType": "violet",
	"border": "blue", "mdLink": "blue", "syntaxKeyword": "blue",
	"syntaxVariable": "calamine",
	"success":        "green", "mdCodeBlock": "green", "toolDiffAdded": "green",
	"bashMode": "green", "syntaxNumber": "green",
	"error": "red", "toolDiffRemoved": "red",
	"warning": "yellow", "mdHeading": "yellow", "syntaxFunction": "yellow",
	"syntaxString": "orange",
	"thinkingOff":  "neutral", "thinkingMinimal": "thinkingSlate",
	"thinkingLow": "thinkingBlue", "thinkingMedium": "thinkingPeriwinkle",
	"thinkingHigh": "thinkingViolet", "thinkingXhigh": "thinkingMagenta",
	"thinkingMax": "thinkingRed",
}

// systemTokenSlots overrides the family slot for tokens that would otherwise
// share a hue with a similar token.
var systemTokenSlots = map[string]int{"syntaxString": 2, "syntaxNumber": 5, "searchMatchBg": 3}

// systemCurve is a target-lightness curve: a polynomial in the surface's OKLab
// lightness giving the OKLab lightness a token needs on it. `reachable` is the
// surface lightness range where the level can be reached.
type systemCurve struct {
	coefficients [6]float64
	reachable    [2]float64
}

func systemCurveOf(coefficients [6]float64, low, high float64) systemCurve {
	return systemCurve{coefficients: coefficients, reachable: [2]float64{low, high}}
}

var systemLevels = map[string]map[string]systemCurve{
	"panel": {
		"dark":  systemCurveOf([6]float64{0.29131, -0.39746, 2.33185, -0.85524, -1.2076, 0.86276}, 0, 0.979),
		"light": systemCurveOf([6]float64{-3.74073, 27.94549, -78.44258, 112.6798, -79.60015, 22.11277}, 0.348, 1),
	},
	"track": {
		"dark":  systemCurveOf([6]float64{0.39028, -0.23015, 0.83573, 2.43829, -4.38292, 2.01582}, 0, 0.946),
		"light": systemCurveOf([6]float64{-5.24921, 38.37322, -107.28833, 152.10005, -106.17127, 29.18061}, 0.368, 1),
	},
	"thinking0": {
		"dark":  systemCurveOf([6]float64{0.52988, -0.05809, -0.30924, 4.63567, -6.52933, 2.89108}, 0, 0.873),
		"light": systemCurveOf([6]float64{-28.27749, 182.85284, -469.62416, 603.15916, -384.59976, 97.35147}, 0.51, 1),
	},
	"thinking1": {
		"dark":  systemCurveOf([6]float64{0.55278, -0.03667, -0.45659, 4.95347, -6.90265, 3.0706}, 0, 0.858),
		"light": systemCurveOf([6]float64{-37.10484, 235.86282, -596.62344, 754.3633, -474.00763, 118.3551}, 0.535, 1),
	},
	"thinking2": {
		"dark":  systemCurveOf([6]float64{0.57486, -0.01765, -0.58987, 5.25227, -7.27175, 3.25532}, 0, 0.842),
		"light": systemCurveOf([6]float64{-59.89653, 377.05024, -945.07843, 1182.03145, -734.96375, 181.68658}, 0.556, 1),
	},
	"thinking3": {
		"dark":  systemCurveOf([6]float64{0.59621, -0.00062, -0.71148, 5.53588, -7.6392, 3.44606}, 0, 0.827),
		"light": systemCurveOf([6]float64{-72.07122, 445.84082, -1099.57352, 1353.88793, -829.53392, 202.26164}, 0.58, 1),
	},
	"thinking4": {
		"dark":  systemCurveOf([6]float64{0.61691, 0.01462, -0.82288, 5.80651, -8.00641, 3.64333}, 0, 0.811),
		"light": systemCurveOf([6]float64{-110.14338, 674.21488, -1645.75941, 2004.32367, -1215.15899, 293.3183}, 0.6, 1),
	},
	"thinking5": {
		"dark":  systemCurveOf([6]float64{0.63702, 0.02826, -0.92498, 6.06465, -8.37246, 3.84651}, 0, 0.795),
		"light": systemCurveOf([6]float64{-175.47701, 1063.54495, -2570.70594, 3098.80776, -1860.15527, 444.76392}, 0.62, 1),
	},
	"thinking6": {
		"dark":  systemCurveOf([6]float64{0.65658, 0.04044, -1.01835, 6.30989, -8.73529, 4.05439}, 0, 0.779),
		"light": systemCurveOf([6]float64{-183.81712, 1094.70055, -2602.68539, 3088.71276, -1826.91131, 430.75931}, 0.643, 1),
	},
	"subtle": {
		"dark":  systemCurveOf([6]float64{0.56762, -0.02475, -0.5383, 5.12628, -7.10931, 3.17324}, 0, 0.848),
		"light": systemCurveOf([6]float64{-232.85459, 1376.54473, -3249.11801, 3827.91186, -2248.29472, 526.55751}, 0.657, 1),
	},
	"thumb": {
		"dark":  systemCurveOf([6]float64{0.60323, 0.00278, -0.73328, 5.57157, -7.68067, 3.46933}, 0, 0.823),
		"light": systemCurveOf([6]float64{-82.89897, 511.01355, -1255.98095, 1540.76821, -940.68087, 228.58523}, 0.586, 1),
	},
	"readable": {
		"dark":  systemCurveOf([6]float64{0.66937, 0.04704, -1.06871, 6.43941, -8.9332, 4.17229}, 0, 0.77),
		"light": systemCurveOf([6]float64{-1554.52576, 8733.56817, -19604.93507, 21977.72696, -12300.99599, 2749.81288}, 0.751, 1),
	},
	"emphasis": {
		"dark":  systemCurveOf([6]float64{0.7303, 0.07695, -1.31626, 7.1681, -10.14436, 4.92846}, 0, 0.712),
		"light": systemCurveOf([6]float64{-4948.31942, 26870.91986, -58334.48399, 63280.17197, -34298.01053, 7430.30146}, 0.811, 1),
	},
	"textOnPanel": {
		"dark":  systemCurveOf([6]float64{0.86713, 0.05232, -0.89428, 4.79014, -5.5432, 1.75023}, 0, 0.542),
		"light": systemCurveOf([6]float64{-8570.89457, 43954.60805, -90084.00702, 92220.6791, -47152.15802, 9632.27113}, 0.867, 1),
	},
	"text": {
		"dark":  systemCurveOf([6]float64{0.89242, 0.02311, -0.44862, 2.34417, -0.06084, -2.63844}, 0, 0.5),
		"light": systemCurveOf([6]float64{-2004.67048, 6664.47299, -6060.70202, -1792.61209, 5133.82359, -1939.85583}, 0.894, 1),
	},
}

type systemSurface = string

type systemRule struct {
	token string
	on    []systemSurface
	level string
}

var (
	systemToolPanels    = []systemSurface{"toolPendingBg", "toolSuccessBg", "toolErrorBg"}
	systemMessagePanels = []systemSurface{"userMessageBg", "customMessageBg"}
	systemPanelTokens   = []string{
		"userMessageBg", "toolPendingBg", "toolSuccessBg", "toolErrorBg",
		"selectedBg", "searchMatchBg", "customMessageBg",
	}
	systemThinkingTokens = []string{
		"thinkingOff", "thinkingMinimal", "thinkingLow", "thinkingMedium",
		"thinkingHigh", "thinkingXhigh", "thinkingMax",
	}
	systemThinkingLevels = []string{
		"thinking0", "thinking1", "thinking2", "thinking3", "thinking4",
		"thinking5", "thinking6",
	}
)

// systemRules is upstream's RULES table.
var systemRules = func() []systemRule {
	rules := []systemRule{}
	each := func(tokens []string, on []systemSurface, level string) {
		for _, token := range tokens {
			rules = append(rules, systemRule{token: token, on: on, level: level})
		}
	}
	withTools := func(prefix ...string) []systemSurface {
		out := append([]string{}, prefix...)
		return append(out, systemToolPanels...)
	}
	withMessagesAndTools := func(prefix ...string) []systemSurface {
		out := withTools(prefix...)
		return append(out, systemMessagePanels...)
	}
	each(systemPanelTokens, []systemSurface{"background"}, "panel")
	rules = append(rules,
		systemRule{"text", []systemSurface{"background"}, "text"},
		systemRule{"text", []systemSurface{"selectedBg"}, "textOnPanel"},
		systemRule{"userMessageText", []systemSurface{"userMessageBg"}, "textOnPanel"},
		systemRule{"toolTitle", systemToolPanels, "textOnPanel"},
	)
	each([]string{"accent", "success", "error", "warning"}, withTools("background", "selectedBg"), "readable")
	rules = append(rules,
		systemRule{"muted", withTools("background", "selectedBg", "customMessageBg"), "readable"},
		systemRule{"dim", withTools("background", "selectedBg", "customMessageBg"), "subtle"},
		systemRule{"thinkingText", []systemSurface{"background"}, "readable"},
		systemRule{"customMessageText", withTools("customMessageBg"), "readable"},
		systemRule{"customMessageLabel", withTools("background", "customMessageBg", "selectedBg"), "readable"},
		systemRule{"toolOutput", withTools("background"), "readable"},
	)
	each([]string{"mdHeading", "mdLink", "mdLinkUrl", "mdCode", "mdQuote", "mdCodeBlockBorder", "mdListBullet"}, append([]systemSurface{"background"}, systemMessagePanels...), "readable")
	rules = append(rules,
		systemRule{"mdCodeBlock", append([]systemSurface{"background"}, withMessagesAndTools()...), "readable"},
	)
	each([]string{"toolDiffAdded", "toolDiffRemoved", "toolDiffContext"}, withTools("background"), "readable")
	each([]string{"syntaxComment", "syntaxKeyword", "syntaxFunction", "syntaxVariable", "syntaxString", "syntaxNumber", "syntaxType", "syntaxOperator", "syntaxPunctuation"}, append([]systemSurface{"background"}, withMessagesAndTools()...), "readable")
	rules = append(rules,
		systemRule{"searchMatchText", []systemSurface{"searchMatchBg"}, "readable"},
	)
	each([]string{"bashMode", "border", "borderAccent"}, []systemSurface{"background"}, "readable")
	rules = append(rules, systemRule{"borderMuted", []systemSurface{"background"}, "subtle"})
	each([]string{"mdQuoteBorder", "mdHr"}, append([]systemSurface{"background"}, withMessagesAndTools()...), "readable")
	rules = append(rules,
		systemRule{"scrollbarTrack", []systemSurface{"background"}, "track"},
		systemRule{"scrollbarThumb", []systemSurface{"scrollbarTrack"}, "thumb"},
	)
	for index, token := range systemThinkingTokens {
		rules = append(rules, systemRule{token, []systemSurface{"background"}, systemThinkingLevels[index]})
	}
	return rules
}()

// systemSolveOrder lists tokens in dependency order: every surface before the
// tokens drawn on it.
var systemSolveOrder = func() []string {
	order := []string{}
	var visit func(token string)
	visit = func(token string) {
		for _, existing := range order {
			if existing == token {
				return
			}
		}
		for _, rule := range systemRules {
			if rule.token != token {
				continue
			}
			for _, surface := range rule.on {
				if surface != "background" {
					visit(surface)
				}
			}
		}
		order = append(order, token)
	}
	for _, rule := range systemRules {
		visit(rule.token)
	}
	return order
}()

// systemReadableFloor is the level stronger levels are compressed toward.
var systemReadableFloor = map[string]string{"dark": "readable", "light": "subtle"}

// systemForegroundLevel is the level body text must reach to use the terminal's
// foreground.
const systemForegroundLevel = "emphasis"

// systemForegroundTokens are the text-level tokens that may take the terminal's
// foreground.
var systemForegroundTokens = []string{"text", "userMessageText", "toolTitle"}

// systemTextMinimumContrast is the WCAG 2 ratio body text must reach.
const systemTextMinimumContrast = 4.5

// SystemThemeInput is what the terminal reported: its colors, the palette's 16
// entries, a saturation multiplier and an appearance hint.
type SystemThemeInput struct {
	Foreground     *tui.RgbColor
	Background     *tui.RgbColor
	Palette        []tui.RgbColor
	Saturation     *float64
	AppearanceHint string
}

// SystemThemeColors is the generated theme: hex strings or ANSI indices per
// token ("" for the terminal default), the tokens rendered faint, and the
// resolved appearance.
type SystemThemeColors struct {
	Colors     map[string]any
	Dim        []string
	Appearance string
}

// RelativeLuminance is the WCAG 2 relative luminance.
func RelativeLuminance(color tui.RgbColor) float64 {
	linear := func(channel int) float64 {
		value := float64(channel) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(color.R) + 0.7152*linear(color.G) + 0.0722*linear(color.B)
}

// WcagContrast is the WCAG 2 contrast ratio, 1-21.
func WcagContrast(first, second tui.RgbColor) float64 {
	a := RelativeLuminance(first)
	b := RelativeLuminance(second)
	return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
}

// TerminalAppearance decides dark or light from the terminal's reported colors.
func TerminalAppearance(background tui.RgbColor, foreground *tui.RgbColor) string {
	white := tui.RgbColor{R: 255, G: 255, B: 255}
	black := tui.RgbColor{}
	whiteContrast := WcagContrast(white, background)
	blackContrast := WcagContrast(black, background)
	if foreground != nil {
		foregroundL := systemOklabLightness(*foreground)
		backgroundL := systemOklabLightness(background)
		if math.Abs(foregroundL-backgroundL) > 0.05 {
			appearance := "light"
			if foregroundL > backgroundL {
				appearance = "dark"
			}
			best := blackContrast
			if appearance == "dark" {
				best = whiteContrast
			}
			if best >= systemTextMinimumContrast {
				return appearance
			}
		}
	}
	if whiteContrast >= blackContrast {
		return "dark"
	}
	return "light"
}

func systemOklabLightness(color tui.RgbColor) float64 { return tui.RgbToOklch(color).L }

func systemClamp(value, low, high float64) float64 { return math.Min(high, math.Max(low, value)) }

func systemHexOf(color tui.RgbColor) string {
	return fmt.Sprintf("#%02x%02x%02x", systemClampInt(color.R), systemClampInt(color.G), systemClampInt(color.B))
}

func systemClampInt(value int) int { return max(0, min(255, value)) }

// systemBellWeight is a Gaussian (center 0.5, sigma 0.25), 0 at black and white,
// 1 in the middle.
func systemBellWeight(lightness float64) float64 {
	gaussian := func(x float64) float64 { return math.Exp(-((x - 0.5) * (x - 0.5)) / (2 * 0.25 * 0.25)) }
	return (gaussian(lightness) - gaussian(0)) / (1 - gaussian(0))
}

// systemSaturationCurve is a family's saturation relative to its maximum: 1 at
// mid lightness, min/max at black and white.
func systemSaturationCurve(family systemFamily, lightness float64) float64 {
	floor := 1.0
	if family.satMax > 0 {
		floor = family.satMin / family.satMax
	}
	return floor + (1-floor)*systemBellWeight(lightness)
}

// systemLevelTarget is the target lightness for a level on a surface, or false
// where the level cannot be reached.
func systemLevelTarget(level, appearance string, surfaceL float64) (float64, bool) {
	curve := systemLevels[level][appearance]
	if surfaceL < curve.reachable[0] || surfaceL > curve.reachable[1] {
		return 0, false
	}
	sum := 0.0
	for power, coefficient := range curve.coefficients {
		sum += coefficient * math.Pow(surfaceL, float64(power))
	}
	return sum, true
}

func systemHasString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func systemIsPanel(token string) bool { return systemHasString(systemPanelTokens, token) }

func systemOkhslOf(color tui.RgbColor) tui.OkhslChannels { return tui.RgbToOkhsl(color) }

// systemAnchored is a source color's hue at another OKHSL lightness, with its
// saturation falling off along the family's curve.
func systemAnchored(source tui.OkhslChannels, family systemFamily, lightness, saturation float64) tui.RgbColor {
	anchor := systemSaturationCurve(family, source.L)
	falloff := 1.0
	if anchor > 0 {
		falloff = math.Min(1, systemSaturationCurve(family, lightness)/anchor)
	}
	return tui.OkhslToRgb(source.H, source.S*falloff*saturation, lightness)
}

// systemWithTextContrast moves a text color toward white or black until it
// reaches the WCAG minimum on every surface.
func systemWithTextContrast(color tui.RgbColor, surfaces []tui.RgbColor, lighter bool) tui.RgbColor {
	meets := func(candidate tui.RgbColor) bool {
		for _, surface := range surfaces {
			if WcagContrast(candidate, surface) < systemTextMinimumContrast {
				return false
			}
		}
		return true
	}
	if meets(color) {
		return color
	}
	channels := systemOkhslOf(color)
	at := func(lightness float64) tui.RgbColor { return tui.OkhslToRgb(channels.H, channels.S, lightness) }
	extreme := 0.0
	if lighter {
		extreme = 1
	}
	if !meets(at(extreme)) {
		return at(extreme)
	}
	low, high := channels.L, extreme
	for index := 0; index < 20; index++ {
		middle := (low + high) / 2
		if meets(at(middle)) {
			high = middle
		} else {
			low = middle
		}
	}
	return at(high)
}

// GenerateSystemThemeColors derives the system theme's colors from the
// terminal's reported colors.
func GenerateSystemThemeColors(input SystemThemeInput) SystemThemeColors {
	saturation := 1.0
	if input.Saturation != nil {
		saturation = systemClamp(*input.Saturation, 0, 1)
	}
	if input.Background == nil {
		return systemIndexedColors(saturation, input.AppearanceHint)
	}
	background := *input.Background
	var palette []tui.OkhslChannels
	if len(input.Palette) == 16 {
		palette = make([]tui.OkhslChannels, 0, 16)
		for _, color := range input.Palette {
			palette = append(palette, systemOkhslOf(color))
		}
	}
	appearance := TerminalAppearance(background, input.Foreground)
	lighter := appearance == "dark"
	extreme := 0.0
	if lighter {
		extreme = 1
	}
	backgroundL := systemOklabLightness(background)

	paint := func(token string, oklabL float64) tui.RgbColor {
		lightness := tui.OklabToOkhslLightness(oklabL)
		family := systemFamilies[systemTokenFamilies[token]]
		if palette == nil {
			return tui.OkhslToRgb(family.hue, (family.satMin+(family.satMax-family.satMin)*systemBellWeight(lightness))*saturation, lightness)
		}
		slot := family.slot
		if override, ok := systemTokenSlots[token]; ok {
			slot = override
		}
		return systemAnchored(palette[slot], family, lightness, saturation)
	}

	target := func(level string, surfaceL, t float64) (float64, bool) {
		reached, ok := systemLevelTarget(level, appearance, surfaceL)
		if !ok && t == 0 {
			return 0, false
		}
		reachedValue := reached
		if !ok {
			reachedValue = extreme
		}
		distance := reachedValue - surfaceL
		floorReached, floorOK := systemLevelTarget(systemReadableFloor[appearance], appearance, surfaceL)
		floorValue := floorReached
		if !floorOK {
			floorValue = extreme
		}
		floor := floorValue - surfaceL
		compressed := distance
		if math.Abs(distance) > math.Abs(floor) {
			compressed = distance - (distance-floor)*math.Min(t, 1)
		}
		return surfaceL + compressed*(1-math.Max(0, t-1)), true
	}

	extremeText := tui.RgbColor{}
	if lighter {
		extremeText = tui.RgbColor{R: 255, G: 255, B: 255}
	}
	readable := func(color tui.RgbColor) bool {
		return WcagContrast(extremeText, color) >= systemTextMinimumContrast
	}
	limitPanel := func(token string, l float64) tui.RgbColor {
		color := paint(token, l)
		if readable(color) {
			return color
		}
		low, high := backgroundL, l
		for index := 0; index < 20; index++ {
			middle := (low + high) / 2
			if readable(paint(token, middle)) {
				low = middle
			} else {
				high = middle
			}
		}
		return paint(token, low)
	}

	solve := func(t float64) (map[string]tui.RgbColor, bool) {
		colors := map[string]tui.RgbColor{"background": background}
		for _, token := range systemSolveOrder {
			targets := []float64{}
			for _, rule := range systemRules {
				if rule.token != token {
					continue
				}
				for _, surface := range rule.on {
					surfaceColor, ok := colors[surface]
					if !ok {
						surfaceColor = background
					}
					value, ok := target(rule.level, systemOklabLightness(surfaceColor), t)
					if !ok || value < 0 || value > 1 {
						return nil, false
					}
					targets = append(targets, value)
				}
			}
			l := targets[0]
			for _, value := range targets[1:] {
				if lighter {
					l = math.Max(l, value)
				} else {
					l = math.Min(l, value)
				}
			}
			if systemIsPanel(token) {
				colors[token] = limitPanel(token, l)
			} else {
				colors[token] = paint(token, l)
			}
		}
		return colors, true
	}

	relaxation := 0.0
	colors, ok := solve(0)
	if !ok {
		low, high := 0.0, 2.0
		colors, ok = solve(high)
		for index := 0; index < 20; index++ {
			middle := (low + high) / 2
			if attempt, attemptOK := solve(middle); attemptOK {
				high = middle
				colors = attempt
			} else {
				low = middle
			}
		}
		relaxation = high
	}
	if colors == nil {
		colors = map[string]tui.RgbColor{}
	}
	surfacesOf := func(token string) []tui.RgbColor {
		surfaces := []tui.RgbColor{}
		for _, rule := range systemRules {
			if rule.token != token {
				continue
			}
			for _, surface := range rule.on {
				color, ok := colors[surface]
				if !ok {
					color = background
				}
				surfaces = append(surfaces, color)
			}
		}
		return surfaces
	}

	result := map[string]any{}
	for _, token := range systemTokenOrder {
		if color, ok := colors[token]; ok {
			result[token] = systemHexOf(color)
		} else {
			result[token] = ""
		}
	}
	for _, token := range systemForegroundTokens {
		surfaces := surfacesOf(token)
		text, hasText := colors[token]
		if input.Foreground != nil {
			targets := make([]float64, 0, len(surfaces))
			allOK := true
			for _, surface := range surfaces {
				value, ok := target(systemForegroundLevel, systemOklabLightness(surface), relaxation)
				if !ok || value < 0 || value > 1 {
					allOK = false
					break
				}
				targets = append(targets, value)
			}
			if allOK {
				needed := targets[0]
				for _, value := range targets[1:] {
					if lighter {
						needed = math.Max(needed, value)
					} else {
						needed = math.Min(needed, value)
					}
				}
				foregroundL := systemOklabLightness(*input.Foreground)
				if (lighter && foregroundL >= needed) || (!lighter && foregroundL <= needed) {
					result[token] = ""
					continue
				}
				text = systemAnchored(systemOkhslOf(*input.Foreground), systemFamilies["neutral"], tui.OklabToOkhslLightness(needed), saturation)
				hasText = true
			}
		}
		if hasText {
			result[token] = systemHexOf(systemWithTextContrast(text, surfaces, lighter))
		}
	}
	return SystemThemeColors{Colors: result, Dim: []string{}, Appearance: appearance}
}

// systemIndexedColors is the tier for terminals that reported nothing: ANSI
// indices and the default colors, with neutral tokens below body text faint.
func systemIndexedColors(saturation float64, appearanceHint string) SystemThemeColors {
	colors := map[string]any{}
	dim := []string{}
	for _, token := range systemTokenOrder {
		familyName := systemTokenFamilies[token]
		if systemIsPanel(token) {
			colors[token] = ""
			continue
		}
		neutral := familyName == "neutral"
		if !neutral && saturation > 0 {
			slot := systemFamilies[familyName].slot
			if override, ok := systemTokenSlots[token]; ok {
				slot = override
			}
			colors[token] = slot
		} else {
			colors[token] = ""
		}
		if neutral && !systemHasString(systemForegroundTokens, token) {
			dim = append(dim, token)
		}
	}
	return SystemThemeColors{Colors: colors, Dim: dim, Appearance: appearanceHint}
}
