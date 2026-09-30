package tui

import (
	"math"
	"regexp"
	"strconv"
)

// Oklab and OKHSL <-> sRGB conversion, ported from packages/tui/src/oklab.ts
// (upstream bf8e4b953). OKHSL's saturation is relative to the sRGB gamut at each
// hue and lightness. The reference implementation is Björn Ottosson's
// (https://bottosson.github.io/posts/colorpicker/), Copyright (c) 2021 Björn
// Ottosson, MIT licensed. It lives in tui because the theme system builds its
// OKLCH/OKHSL colors and mixing on it.

type okVector [3]float64
type okMatrix [3][3]float64

func okMultiply(m okMatrix, v okVector) okVector {
	return okVector{
		m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2],
		m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2],
	}
}

var (
	linearSrgbToLms = okMatrix{
		{0.4122214694707629, 0.5363325372617349, 0.0514459932675022},
		{0.2119034958178251, 0.6806995506452344, 0.1073969535369405},
		{0.0883024591900564, 0.2817188391361215, 0.6299787016738222},
	}
	lmsToLab = okMatrix{
		{0.210454268309314, 0.793617774702305, -0.0040720430116193},
		{1.9779985324311684, -2.42859224204858, 0.450593709617411},
		{0.0259040424655478, 0.7827717124575296, -0.8086757549230774},
	}
	labToLms = okMatrix{
		{1, 0.3963377773761749, 0.2158037573099136},
		{1, -0.1055613458156586, -0.0638541728258133},
		{1, -0.0894841775298119, -1.2914855480194092},
	}
	lmsToLinearSrgb = okMatrix{
		{4.0767416360759583, -3.3077115392580629, 0.2309699031821043},
		{-1.2684379732850315, 2.6097573492876882, -0.341319376002657},
		{-0.0041960761386756, -0.7034186179359362, 1.7076146940746117},
	}
)

// saturationFit holds, per sRGB channel, the (a, b) half-plane where the channel
// clips first and the polynomial approximating the maximum saturation there.
var saturationFit = [3]struct {
	ab   [2]float64
	poly [5]float64
}{
	{[2]float64{-1.8817031, -0.80936501}, [5]float64{1.19086277, 1.76576728, 0.59662641, 0.75515197, 0.56771245}},
	{[2]float64{1.8144408, -1.19445267}, [5]float64{0.73956515, -0.45954404, 0.08285427, 0.12541073, -0.14503204}},
	{[2]float64{0.13110758, 1.81333971}, [5]float64{1.35733652, -0.00915799, -1.1513021, -0.50559606, 0.00692167}},
}

const (
	okK1 = 0.206
	okK2 = 0.03
	okK3 = (1 + okK1) / (1 + okK2)
)

// OklabToOkhslLightness converts Oklab lightness to OKHSL lightness.
func OklabToOkhslLightness(x float64) float64 {
	t := okK3*x - okK1
	return 0.5 * (t + math.Sqrt(t*t+4*okK2*okK3*x))
}

// okhslToOklabLightness converts OKHSL lightness to Oklab lightness.
func okhslToOklabLightness(x float64) float64 { return (x*x + okK1*x) / (okK3 * (x + okK2)) }

// linearToSrgb is the sRGB transfer function: linear to encoded, both 0-1.
func linearToSrgb(value float64) float64 {
	if value > 0.0031308 {
		return 1.055*math.Pow(value, 1.0/2.4) - 0.055
	}
	return 12.92 * value
}

// srgbToLinear is the inverse sRGB transfer function, both 0-1.
func srgbToLinear(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

// oklabToLinearSrgb converts Oklab [L, a, b] to linear sRGB (0-1, may leave the
// gamut).
func oklabToLinearSrgb(lab okVector) okVector {
	lms := okMultiply(labToLms, lab)
	return okMultiply(lmsToLinearSrgb, okVector{lms[0] * lms[0] * lms[0], lms[1] * lms[1] * lms[1], lms[2] * lms[2] * lms[2]})
}

// linearSrgbToOklab converts linear sRGB (0-1) to Oklab [L, a, b].
func linearSrgbToOklab(rgb okVector) okVector {
	lms := okMultiply(linearSrgbToLms, rgb)
	return okMultiply(lmsToLab, okVector{math.Cbrt(lms[0]), math.Cbrt(lms[1]), math.Cbrt(lms[2])})
}

// RgbToOklab converts sRGB channels (0-255) to Oklab [L, a, b].
func RgbToOklab(color RgbColor) okVector {
	return linearSrgbToOklab(okVector{
		srgbToLinear(float64(color.R) / 255),
		srgbToLinear(float64(color.G) / 255),
		srgbToLinear(float64(color.B) / 255),
	})
}

// linearSrgbToRgb converts linear sRGB to sRGB channels (0-255, rounded),
// clipping out-of-gamut channels.
func linearSrgbToRgb(linear okVector) RgbColor {
	channel := func(value float64) int {
		return int(math.Round(math.Min(1, math.Max(0, linearToSrgb(value))) * 255))
	}
	return RgbColor{R: channel(linear[0]), G: channel(linear[1]), B: channel(linear[2])}
}

// lmsSlopes is the rate of change of each cube-root LMS component along a chroma
// direction (a, b).
func lmsSlopes(a, b float64) okVector {
	return okVector{
		labToLms[0][1]*a + labToLms[0][2]*b,
		labToLms[1][1]*a + labToLms[1][2]*b,
		labToLms[2][1]*a + labToLms[2][2]*b,
	}
}

// maxSaturation is the largest saturation (C/L) inside sRGB for hue (a, b): the
// polynomial fit plus one Halley step.
func maxSaturation(a, b float64) float64 {
	channel := 0
	for index, fit := range saturationFit {
		if index == 2 || fit.ab[0]*a+fit.ab[1]*b > 1 {
			channel = index
			break
		}
	}
	poly := saturationFit[channel].poly
	weights := lmsToLinearSrgb[channel]
	saturation := poly[0] + poly[1]*a + poly[2]*b + poly[3]*a*a + poly[4]*a*b

	slopes := lmsSlopes(a, b)
	base := okVector{1 + saturation*slopes[0], 1 + saturation*slopes[1], 1 + saturation*slopes[2]}
	dot := func(values okVector) float64 {
		return weights[0]*values[0] + weights[1]*values[1] + weights[2]*values[2]
	}
	f := dot(okVector{base[0] * base[0] * base[0], base[1] * base[1] * base[1], base[2] * base[2] * base[2]})
	f1 := dot(okVector{
		3 * slopes[0] * base[0] * base[0],
		3 * slopes[1] * base[1] * base[1],
		3 * slopes[2] * base[2] * base[2],
	})
	f2 := dot(okVector{
		6 * slopes[0] * slopes[0] * base[0],
		6 * slopes[1] * slopes[1] * base[1],
		6 * slopes[2] * slopes[2] * base[2],
	})
	return saturation - (f*f1)/(f1*f1-0.5*f*f2)
}

// okCusp returns the Oklab lightness and chroma of the most saturated sRGB color
// of hue (a, b).
func okCusp(a, b float64) [2]float64 {
	saturation := maxSaturation(a, b)
	linear := oklabToLinearSrgb(okVector{1, saturation * a, saturation * b})
	peak := math.Max(linear[0], math.Max(linear[1], linear[2]))
	lightness := math.Cbrt(1 / peak)
	return [2]float64{lightness, lightness * saturation}
}

// okMaxChroma is the chroma where the constant-lightness line at `lightness`
// leaves the sRGB gamut.
func okMaxChroma(a, b, lightness float64, cusp [2]float64) float64 {
	cuspL, cuspC := cusp[0], cusp[1]
	if lightness <= cuspL {
		return cuspC * lightness / cuspL
	}
	t := cuspC * (lightness - 1) / (cuspL - 1)
	slopes := lmsSlopes(a, b)
	lms := okVector{lightness + t*slopes[0], lightness + t*slopes[1], lightness + t*slopes[2]}
	cubes := okVector{lms[0] * lms[0] * lms[0], lms[1] * lms[1] * lms[1], lms[2] * lms[2] * lms[2]}
	first := okVector{
		3 * slopes[0] * lms[0] * lms[0],
		3 * slopes[1] * lms[1] * lms[1],
		3 * slopes[2] * lms[2] * lms[2],
	}
	second := okVector{
		6 * slopes[0] * slopes[0] * lms[0],
		6 * slopes[1] * slopes[1] * lms[1],
		6 * slopes[2] * slopes[2] * lms[2],
	}
	minimum := math.MaxFloat64
	for _, row := range lmsToLinearSrgb {
		dot := func(values okVector) float64 { return row[0]*values[0] + row[1]*values[1] + row[2]*values[2] }
		f := dot(cubes) - 1
		f1 := dot(first)
		f2 := dot(second)
		u := f1 / (f1*f1 - 0.5*f*f2)
		step := math.MaxFloat64
		if u >= 0 {
			step = -f * u
		}
		if step < minimum {
			minimum = step
		}
	}
	return t + minimum
}

// okChromaStops returns OKHSL's chroma reference points at lightness L and hue
// (a, b): [c0, cMid, cMax].
func okChromaStops(L, a, b float64) [3]float64 {
	peak := okCusp(a, b)
	cMax := okMaxChroma(a, b, L, peak)
	k := cMax / math.Min(L*(peak[1]/peak[0]), (1-L)*(peak[1]/(1-peak[0])))
	midS := 0.11516993 +
		1/(7.4477897+
			4.1590124*b+
			a*(-2.19557347+
				1.75198401*b+
				a*(-2.13704948-10.02301043*b+a*(-4.24894561+5.38770819*b+4.69891013*a))))
	midT := 0.11239642 +
		1/(1.6132032-
			0.68124379*b+
			a*(0.40370612+
				0.90148123*b+
				a*(-0.27087943+0.6122399*b+a*(0.00299215-0.45399568*b-0.14661872*a))))
	cMid := 0.9 * k * math.Sqrt(math.Sqrt(1/(math.Pow(L*midS, -4)+math.Pow((1-L)*midT, -4))))
	c0 := math.Sqrt(1 / (math.Pow(L*0.4, -2) + math.Pow((1-L)*0.8, -2)))
	return [3]float64{c0, cMid, cMax}
}

// OkhslChannels are OKHSL channels: hue in degrees (0 for grays), saturation and
// lightness 0-1.
type OkhslChannels struct {
	H float64
	S float64
	L float64
}

// OklchChannels are OKLCH channels: Oklab lightness, chroma and hue in degrees.
type OklchChannels struct {
	L float64
	C float64
	H float64
}

// RgbToOklch converts sRGB channels (0-255) to OKLCH.
func RgbToOklch(color RgbColor) OklchChannels {
	lab := RgbToOklab(color)
	return OklchChannels{
		L: lab[0],
		C: math.Hypot(lab[1], lab[2]),
		H: math.Mod(math.Atan2(lab[2], lab[1])*180/math.Pi+360, 360),
	}
}

const colorNumberPattern = `[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?`

var (
	themeHexColorPattern = regexp.MustCompile(`(?i)^#([0-9a-f]{3}|[0-9a-f]{6})$`)
	oklchColorPattern    = regexp.MustCompile(`(?i)^oklch\(\s*(` + colorNumberPattern + `)(%)?\s+(` + colorNumberPattern + `)\s+(` + colorNumberPattern + `)(?:deg)?\s*\)$`)
	okhslColorPattern    = regexp.MustCompile(`(?i)^okhsl\(\s*(` + colorNumberPattern + `)(?:deg)?\s+(` + colorNumberPattern + `)(%)?\s+(` + colorNumberPattern + `)(%)?\s*\)$`)
)

func colorClamp01(value float64) float64 { return math.Min(1, math.Max(0, value)) }

func colorParseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

// OklchToRgb gamut-maps an OKLCH color to sRGB, keeping the hue and reducing
// chroma until the color fits (upstream oklchToRgb).
func OklchToRgb(l, c, h float64) RgbColor {
	l = colorClamp01(l)
	if c < 0 {
		c = 0
	}
	h = math.Mod(math.Mod(h, 360)+360, 360)
	radians := h * math.Pi / 180
	cosine := math.Cos(radians)
	sine := math.Sin(radians)
	atChroma := func(chroma float64) okVector {
		return oklabToLinearSrgb(okVector{l, chroma * cosine, chroma * sine})
	}
	direct := atChroma(c)
	if isInSrgbGamut(direct) {
		return linearSrgbToRgb(direct)
	}
	linear := atChroma(0)
	low, high := 0.0, c
	for index := 0; index < 20; index++ {
		chroma := (low + high) / 2
		candidate := atChroma(chroma)
		if isInSrgbGamut(candidate) {
			low = chroma
			linear = candidate
		} else {
			high = chroma
		}
	}
	return linearSrgbToRgb(linear)
}

func isInSrgbGamut(linear okVector) bool {
	const epsilon = 1e-7
	for _, channel := range linear {
		if channel < -epsilon || channel > 1+epsilon {
			return false
		}
	}
	return true
}

// ParseColor parses a theme color value: #rgb/#rrggbb, oklch(...) or okhsl(...).
// It returns ok=false for anything else, such as a variable reference.
func ParseColor(value string) (RgbColor, bool) {
	if match := themeHexColorPattern.FindStringSubmatch(value); match != nil {
		digits := match[1]
		if len(digits) == 3 {
			digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
		}
		r, _ := strconv.ParseInt(digits[0:2], 16, 32)
		g, _ := strconv.ParseInt(digits[2:4], 16, 32)
		b, _ := strconv.ParseInt(digits[4:6], 16, 32)
		return RgbColor{R: int(r), G: int(g), B: int(b)}, true
	}
	if match := oklchColorPattern.FindStringSubmatch(value); match != nil {
		lightness := colorParseFloat(match[1])
		if match[2] != "" {
			lightness /= 100
		}
		return OklchToRgb(colorClamp01(lightness), math.Max(0, colorParseFloat(match[3])), colorParseFloat(match[4])), true
	}
	if match := okhslColorPattern.FindStringSubmatch(value); match != nil {
		saturation := colorParseFloat(match[2])
		if match[3] != "" {
			saturation /= 100
		}
		lightness := colorParseFloat(match[4])
		if match[5] != "" {
			lightness /= 100
		}
		return OkhslToRgb(colorParseFloat(match[1]), colorClamp01(saturation), colorClamp01(lightness)), true
	}
	return RgbColor{}, false
}

// OkhslToRgb converts OKHSL to sRGB channels (0-255, rounded), clipping
// out-of-gamut channels.
func OkhslToRgb(hue, saturation, lightness float64) RgbColor {
	L := okhslToOklabLightness(lightness)
	lab := okVector{L, 0, 0}
	if L > 0 && L < 1 && saturation > 0 {
		angle := 2 * math.Pi * math.Mod(math.Mod(hue, 360)+360, 360) / 360
		a := math.Cos(angle)
		b := math.Sin(angle)
		stops := okChromaStops(L, a, b)
		c0, cMid, cMax := stops[0], stops[1], stops[2]
		var chroma float64
		if saturation < 0.8 {
			t := 1.25 * saturation
			k1 := 0.8 * c0
			chroma = (t * k1) / (1 - (1-k1/cMid)*t)
		} else {
			t := 5 * (saturation - 0.8)
			k1 := (0.2 * cMid * cMid * 1.25 * 1.25) / c0
			chroma = cMid + (t*k1)/(1-(1-k1/(cMax-cMid))*t)
		}
		lab = okVector{L, chroma * a, chroma * b}
	}
	return linearSrgbToRgb(oklabToLinearSrgb(lab))
}

// RgbToOkhsl converts sRGB channels (0-255) to OKHSL.
func RgbToOkhsl(color RgbColor) OkhslChannels {
	lab := RgbToOklab(color)
	L, labA, labB := lab[0], lab[1], lab[2]
	chroma := math.Hypot(labA, labB)
	lightness := OklabToOkhslLightness(L)
	if chroma < 1e-9 || lightness <= 0 || lightness >= 1 {
		return OkhslChannels{H: 0, S: 0, L: lightness}
	}
	hue := math.Mod(math.Atan2(labB, labA)*180/math.Pi+360, 360)
	stops := okChromaStops(L, labA/chroma, labB/chroma)
	c0, cMid, cMax := stops[0], stops[1], stops[2]
	var saturation float64
	if chroma < cMid {
		k1 := 0.8 * c0
		saturation = 0.8 * (chroma / (k1 + (1-k1/cMid)*chroma))
	} else {
		k1 := (0.2 * cMid * cMid * 1.25 * 1.25) / c0
		offset := chroma - cMid
		saturation = 0.8 + 0.2*(offset/(k1+(1-k1/(cMax-cMid))*offset))
	}
	return OkhslChannels{H: hue, S: math.Min(1, math.Max(0, saturation)), L: lightness}
}
