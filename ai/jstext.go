package ai

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/dat267/pier/internal/jsnum"
)

// utils/sanitize-unicode.ts sanitizeSurrogates has no port. Upstream deletes
// unpaired UTF-16 surrogates from text before sending it to a provider; a Go
// string cannot hold one as text, and the bytes that would encode one are
// invalid UTF-8, which the JSON decoder (D253) and the wire encoder
// (MarshalJSON) both replace with U+FFFD. The provider converters therefore
// pass text through where upstream calls sanitizeSurrogates.

// JSWhitespaceClass is the JavaScript `\s` character class for use inside an
// RE2 pattern: Go's `\s` is `[\t\n\f\r ]`, while JavaScript's also matches the
// Unicode space separators and U+FEFF. Ported patterns that use `\s` build
// their class from this constant so the two engines agree (CR-068/CR-069).
const JSWhitespaceClass = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// JSTrim implements String.prototype.trim over the JavaScript whitespace set
// (jsnum.Whitespace), which keeps U+0085 unlike strings.TrimSpace.
func JSTrim(s string) string { return strings.TrimFunc(s, jsnum.Whitespace) }

// JSTrimLeft implements String.prototype.trimStart.
func JSTrimLeft(s string) string { return strings.TrimLeftFunc(s, jsnum.Whitespace) }

// IsJSSpace reports whether r is in the JS `\s` class.
func IsJSSpace(r rune) bool { return jsnum.Whitespace(r) }

// JSSplitWhitespace splits on runs of the JS `\s` class (String.split(/\s+/)
// with the empty parts dropped), unlike strings.Fields which uses
// unicode.IsSpace.
func JSSplitWhitespace(s string) []string {
	return strings.FieldsFunc(s, jsnum.Whitespace)
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
	if math.Abs(value) >= 1e21 {
		return JSNumberToString(value)
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
//
// ASCII text maps the same under both, so it skips the Caser (D518): model IDs,
// paths and typed queries are lowered on every keystroke.
func JSLower(s string) string {
	for index := range len(s) {
		if s[index] >= utf8.RuneSelf {
			return cases.Lower(language.Und).String(s)
		}
	}
	return strings.ToLower(s)
}

// JSTrimIsEmpty reports whether the JS-trimmed string is empty (the form
// upstream compares against zero, `text.trim().length === 0`).
func JSTrimIsEmpty(s string) bool {
	return JSTrim(s) == ""
}

// JSLength counts UTF-16 code units, matching JavaScript's string.length.
// The canonical implementation lives here; faux re-exports.
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
	before, after, ok := strings.Cut(formatted, "e")
	if !ok {
		return formatted
	}
	mantissa := before
	exponent := after
	sign := ""
	if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
		sign = string(exponent[0])
		exponent = exponent[1:]
	}
	exponent = cmp.Or(strings.TrimLeft(exponent, "0"), "0")
	return mantissa + "e" + sign + exponent
}

// JSSlice slices by UTF-16 code units, matching JavaScript's String.slice.
// A negative start or end counts from the end (String.prototype.slice resolves
// each index as max(length+index, 0) before clamping).
// ownedJSSlice is JSSlice with its bytes copied, so a short excerpt of a large
// provider body does not keep the whole body alive (upstream v1.5.0
// text.ts ownedSlice; Go substrings share the backing array like a V8 slice).
func ownedJSSlice(s string, start, end int) string {
	return strings.Clone(JSSlice(s, start, end))
}

func JSSlice(s string, start, end int) string {
	if start >= 0 && end >= 0 {
		// Both indices are absolute, so nothing after `end` matters and the
		// tail is never measured: a 500-character excerpt of a megabyte error
		// body must not cost the body (D426). The full encode below stays for
		// a negative index, which needs the string's UTF-16 length.
		return jsSliceAbsolute(s, start, end)
	}
	units := utf16.Encode([]rune(s))
	length := len(units)
	if start < 0 {
		start = max(length+start, 0)
	} else if start > length {
		start = length
	}
	if end < 0 {
		end = max(length+end, 0)
	} else if end > length {
		end = length
	}
	if start >= end {
		return ""
	}
	return string(utf16.Decode(units[start:end]))
}

// jsSliceAbsolute implements String.prototype.slice for start >= 0 and
// end >= 0 without encoding the whole string: it walks s once, stops at the
// last unit it needs, and decodes only the kept range. A surrogate pair cut in
// half decodes to U+FFFD, which is what the full encoding holds at that index.
func jsSliceAbsolute(s string, start, end int) string {
	if start >= end {
		return ""
	}
	// A unit is at most 4 bytes of s, so end-start units are a safe cap and the
	// append below never grows. Guard against a caller passing a huge range on a
	// short string.
	capacity := min(end-start, len(s))
	kept := make([]uint16, 0, capacity)
	units := 0
	for _, r := range s {
		if units >= end {
			break
		}
		first, second := uint16(r), uint16(0)
		width := 1
		if r >= 0x10000 {
			high, low := utf16.EncodeRune(r)
			first, second = uint16(high), uint16(low)
			width = 2
		}
		for half := 0; half < width && units < end; half++ {
			if units >= start {
				if half == 0 {
					kept = append(kept, first)
				} else {
					kept = append(kept, second)
				}
			}
			units++
		}
	}
	if len(kept) == 0 {
		return ""
	}
	// utf16.Decode turns a surrogate whose partner was cut off into U+FFFD,
	// which is the character the fully decoded string holds at that index.
	return string(utf16.Decode(kept))
}

// GetPiUserAgent renders the pi user agent: "pi (<platform> <release>; <arch>)"
// (port of utils/pi-user-agent.ts). Node's os.release() is the kernel
// version; Go reads uname where available (os-release mapping is best-effort
// per platform).
func GetPiUserAgent() string {
	release := cmp.Or(kernelRelease(), "unknown")
	return "pi (" + runtimeGOOS() + " " + release + "; " + runtimeGOARCH() + ")"
}

// JSValueToString renders a decoded value the way JavaScript's String(value)
// does: a string as itself, a number through JSNumberToString, a bool as
// "true"/"false", nil as "null", an array as its items joined by commas with
// null and undefined empty, and anything else (including a map) as
// "[object Object]". Callers use it where the reference interpolates a value
// with a template literal or passes it to String().
func JSValueToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return JSNumberToString(typed)
	case float32:
		return JSNumberToString(float64(typed))
	case int:
		return strconv.Itoa(typed)
	case int8:
		return strconv.FormatInt(int64(typed), 10)
	case int16:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case json.Number:
		return typed.String()
	case []any:
		parts := make([]string, len(typed))
		for index, item := range typed {
			// Array.prototype.join renders null and undefined as empty.
			if item != nil {
				parts[index] = JSValueToString(item)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// JSRecoveredError renders a recovered panic the way the reference's
// `error instanceof Error ? error.message : String(error)` does: a Go error
// contributes its message and any other value goes through String(value).
func JSRecoveredError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return errors.New(JSValueToString(recovered))
}
