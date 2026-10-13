package ai

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Port of utils/sanitize-unicode.ts.
//
// In JavaScript, strings are UTF-16 code-unit sequences and unpaired
// surrogates can appear in strings (e.g. from mangled binary data). Go
// strings are UTF-8, where lone surrogates appear as utf8.RuneError (U+FFFD)
// after decoding — the original code units are unrecoverable. Sanitizing
// therefore operates on the UTF-16 re-encoding of the Go string: any unpaired
// surrogate produced there is dropped. For strings decoded from valid UTF-8
// this is a no-op, matching upstream behavior for well-formed input.
//
// D-row D2: JS strings are UTF-16; Go strings are UTF-8. Unpaired surrogates
// cannot survive a Go string round-trip, so the sanitized output can only
// differ from upstream's on inputs that were already corrupt in JS.

// SanitizeSurrogates removes unpaired Unicode surrogate code units from the
// UTF-16 view of the text (upstream sanitizeSurrogates).
func SanitizeSurrogates(text string) string {
	units := utf16.Encode([]rune(text))
	drop := make([]bool, len(units))
	dirty := false
	for i, u := range units {
		if u >= 0xD800 && u <= 0xDBFF { // high surrogate
			if i+1 >= len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				drop[i] = true
				dirty = true
			}
		} else if u >= 0xDC00 && u <= 0xDFFF { // low surrogate
			if i == 0 || units[i-1] < 0xD800 || units[i-1] > 0xDBFF {
				drop[i] = true
				dirty = true
			}
		}
	}
	if !dirty {
		return text
	}
	out := make([]uint16, 0, len(units))
	for i, u := range units {
		if !drop[i] {
			out = append(out, u)
		}
	}
	return string(utf16.Decode(out))
}

// jsSpace is the String.prototype.trim whitespace class (ECMA-402 Whitespace,
// which includes Unicode space separators like U+00A0, U+2028, U+2029, and
// U+FEFF that Go's strings.TrimSpace does not all cover).
var jsSpace = func(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func JSTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// JSTrimLeft implements String.prototype.trimStart.
func JSTrimLeft(s string) string { return strings.TrimLeftFunc(s, jsSpace) }

// IsJSSpace reports whether r is in the JS `\s` class.
func IsJSSpace(r rune) bool { return jsSpace(r) }

// JSSplitWhitespace splits on runs of the JS `\s` class (String.split(/\s+/)
// with the empty parts dropped), unlike strings.Fields which uses
// unicode.IsSpace.
func JSSplitWhitespace(s string) []string {
	return strings.FieldsFunc(s, jsSpace)
}

// JSNumberToFixed formats a number like Number.prototype.toFixed(digits),
// which rounds half away from zero on the exact value of the double (V8), not
// Go's %.nf round-half-to-even. Callers only use small digit counts.
func JSNumberToFixed(value float64, digits int) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	}
	if digits < 0 || digits > 100 {
		digits = 0
	}
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	ratio := new(big.Rat).SetFloat64(value)
	ratio.Mul(ratio, new(big.Rat).SetInt(scale))
	ratio.Add(ratio, big.NewRat(1, 2))
	rounded := new(big.Int).Quo(ratio.Num(), ratio.Denom())
	integerPart := new(big.Int)
	fraction := new(big.Int)
	integerPart.QuoRem(rounded, scale, fraction)
	if digits == 0 {
		return sign + integerPart.String()
	}
	fractionText := fraction.String()
	for len(fractionText) < digits {
		fractionText = "0" + fractionText
	}
	return sign + integerPart.String() + "." + fractionText
}

// JSLower lowercases like String.prototype.toLowerCase: full Unicode case
// mapping with the Greek final-sigma and U+0130 special cases that Go's
// strings.ToLower (simple mapping) omits. The Caser is created per call
// because it is documented as not shareable between goroutines.
func JSLower(s string) string {
	return cases.Lower(language.Und).String(s)
}

// JSTrimIsEmpty reports whether the JS-trimmed string is empty (the form
// upstream compares against zero, `text.trim().length === 0`).
func JSTrimIsEmpty(s string) bool {
	return JSTrim(s) == ""
}

// JSLength counts UTF-16 code units, matching JavaScript's string.length
// (D-row D1). The canonical implementation lives here; faux re-exports.
func JSLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// JSNumberToString formats a number the way JavaScript's String(number) does:
// fixed notation for magnitudes in [1e-6, 1e21), exponential otherwise, with an
// unpadded exponent (Go's %g pads it to two digits and uses different
// thresholds).
func JSNumberToString(f float64) string {
	if f == 0 {
		return "0"
	}
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	formatted := strconv.FormatFloat(f, 'e', -1, 64)
	index := strings.IndexByte(formatted, 'e')
	if index < 0 {
		return formatted
	}
	mantissa := formatted[:index]
	exponent := formatted[index+1:]
	sign := ""
	if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
		sign = string(exponent[0])
		exponent = exponent[1:]
	}
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}
	return mantissa + "e" + sign + exponent
}

// JSSlice slices by UTF-16 code units, matching JavaScript's String.slice.
func JSSlice(s string, start, end int) string {
	units := utf16.Encode([]rune(s))
	if start < 0 {
		start = 0
	}
	if end > len(units) {
		end = len(units)
	}
	if start >= end {
		return ""
	}
	return string(utf16.Decode(units[start:end]))
}

// GetPiUserAgent renders the pi user agent: "pi (<platform> <release>; <arch>)"
// (port of utils/pi-user-agent.ts). Node's os.release() is the kernel
// version; Go reads uname where available (D-row D5: os-release mapping is
// best-effort per platform).
func GetPiUserAgent() string {
	release := kernelRelease()
	if release == "" {
		release = "unknown"
	}
	return "pi (" + runtimeGOOS() + " " + release + "; " + runtimeGOARCH() + ")"
}
