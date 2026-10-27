package highlight

// liveScriptRegexpEnd matches the reference's regexp literal
// `\/(?![ *])(\\.|[^\\\n])*?\/[gim]*(?=\W)`: a slash, a non-space/non-star
// body with backslash escapes, a slash and flags, then a non-word char. The lazy
// body backtracks to a later slash when the lookahead fails, which a
// mode-with-end cannot express.
func liveScriptRegexpEnd(s string, pos int) (int, bool) {
	if pos >= len(s) || s[pos] != '/' {
		return 0, false
	}
	if pos+1 >= len(s) || s[pos+1] == ' ' || s[pos+1] == '*' {
		return 0, false
	}
	for i := pos + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 >= len(s) || s[i+1] == '\n' {
				return 0, false
			}
			i++
		case '\n':
			return 0, false
		case '/':
			j := i + 1
			for j < len(s) && (s[j] == 'g' || s[j] == 'i' || s[j] == 'm') {
				j++
			}
			if j >= len(s) || isWordByte(s[j]) {
				continue
			}
			return j, true
		}
	}
	return 0, false
}

var (
	livescriptM0  *Mode
	livescriptM1  *Mode
	livescriptM2  *Mode
	livescriptM3  *Mode
	livescriptM4  *Mode
	livescriptM5  *Mode
	livescriptM6  *Mode
	livescriptM7  *Mode
	livescriptM8  *Mode
	livescriptM9  *Mode
	livescriptM10 *Mode
	livescriptM11 *Mode
	livescriptM12 *Mode
	livescriptM13 *Mode
	livescriptM14 *Mode
	livescriptM15 *Mode
	livescriptM16 *Mode
	livescriptM17 *Mode
	livescriptM18 *Mode
	livescriptM19 *Mode
	livescriptM20 *Mode
	livescriptM21 *Mode
	livescriptM22 *Mode
	livescriptM23 *Mode
	livescriptM24 *Mode
	livescriptM25 *Mode
	livescriptM26 *Mode
	livescriptM27 *Mode
	livescriptM28 *Mode
	livescriptM29 *Mode
	livescriptM30 *Mode
	livescriptM31 *Mode
	livescriptM32 *Mode
)

func init() {
	livescriptM0 = &Mode{ClassName: "number", Begin: "\\b(0b[01]+)", HasRelevance: true, Relevance: 0}
	livescriptM1 = &Mode{ClassName: "number", Begin: "(\\b0[xX][a-fA-F0-9_]+)|(\\b\\d(\\d|_\\d)*(\\.(\\d(\\d|_\\d)*)?)?(_*[eE]([-+]\\d(_\\d|\\d)*)?)?[_a-z]*)", HasRelevance: true, Relevance: 0}
	livescriptM2 = &Mode{End: "(\\s*/)?", HasRelevance: true, Relevance: 0}
	livescriptM3 = &Mode{ClassName: "string"}
	livescriptM4 = &Mode{ClassName: "string", Begin: "'''", End: "'''"}
	livescriptM5 = &Mode{Begin: "\\\\[\\s\\S]", HasRelevance: true, Relevance: 0}
	livescriptM6 = &Mode{ClassName: "string", Begin: "'", End: "'"}
	livescriptM7 = &Mode{ClassName: "string", Begin: "\"\"\"", End: "\"\"\""}
	livescriptM8 = &Mode{ClassName: "subst", Begin: "#\\{", End: "\\}", Keywords: Keywords{"keyword": "as in of if for while finally var new function do return void else break catch instanceof with throw case default try switch continue typeof delete let yield const class debugger async await static import from export extends then unless until loop of by when and or is isnt not it that otherwise from to til fallthrough case enum native list map __hasProp __extends __slice __bind __indexOf", "literal": "true false null undefined NaN Infinity yes no on off it that void", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}}
	livescriptM9 = &Mode{ClassName: "regexp"}
	livescriptM10 = &Mode{ClassName: "regexp", Begin: "//", End: "//[gim]*"}
	livescriptM11 = &Mode{ClassName: "comment", Begin: "#", End: "$"}
	livescriptM12 = &Mode{Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b"}
	livescriptM13 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	livescriptM14 = &Mode{ClassName: "regexp", ManualBegin: liveScriptRegexpEnd, ManualFirst: "/"}
	livescriptM15 = &Mode{Begin: "@[A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*"}
	livescriptM16 = &Mode{Begin: "``", End: "``", ExcludeBegin: true, ExcludeEnd: true, SubLanguage: []string{"javascript"}}
	livescriptM17 = &Mode{ClassName: "subst", Begin: "#[A-Za-z$_]", End: "(?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*", Keywords: Keywords{"keyword": "as in of if for while finally var new function do return void else break catch instanceof with throw case default try switch continue typeof delete let yield const class debugger async await static import from export extends then unless until loop of by when and or is isnt not it that otherwise from to til fallthrough case enum native list map __hasProp __extends __slice __bind __indexOf", "literal": "true false null undefined NaN Infinity yes no on off it that void", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}}
	livescriptM18 = &Mode{ClassName: "string", Begin: "\"", End: "\""}
	livescriptM19 = &Mode{ClassName: "string", Begin: "\\\\", End: "(\\s|$)", ExcludeEnd: true}
	livescriptM20 = &Mode{ClassName: "comment", Begin: "\\/\\*", End: "\\*\\/"}
	livescriptM21 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	livescriptM22 = &Mode{Begin: "(#=>|=>|\\|>>|-?->|!->)"}
	livescriptM23 = &Mode{ClassName: "function", ReturnBegin: true}
	livescriptM24 = &Mode{ClassName: "function", Begin: "([A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*\\s*(?:=|:=)\\s*)?(\\(.*\\)\\s*)?\\B->\\*?", End: "->\\*?", ReturnBegin: true}
	livescriptM25 = &Mode{ClassName: "title", Begin: "[A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*", HasRelevance: true, Relevance: 0}
	livescriptM26 = &Mode{ClassName: "params", Begin: "\\(", ReturnBegin: true}
	livescriptM27 = &Mode{Begin: "\\(", End: "\\)", Keywords: Keywords{"keyword": "as in of if for while finally var new function do return void else break catch instanceof with throw case default try switch continue typeof delete let yield const class debugger async await static import from export extends then unless until loop of by when and or is isnt not it that otherwise from to til fallthrough case enum native list map __hasProp __extends __slice __bind __indexOf", "literal": "true false null undefined NaN Infinity yes no on off it that void", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}}
	livescriptM28 = &Mode{ClassName: "function", Begin: "([A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*\\s*(?:=|:=)\\s*)?!?(\\(.*\\)\\s*)?\\B[-~]{1,2}>\\*?", End: "[-~]{1,2}>\\*?", ReturnBegin: true}
	livescriptM29 = &Mode{ClassName: "function", Begin: "([A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*\\s*(?:=|:=)\\s*)?(\\(.*\\)\\s*)?\\B!?[-~]{1,2}>\\*?", End: "!?[-~]{1,2}>\\*?", ReturnBegin: true}
	livescriptM30 = &Mode{ClassName: "class", BeginKeywords: "class", Illegal: "[:=\"\\[\\]]", End: "$"}
	livescriptM31 = &Mode{BeginKeywords: "extends", Illegal: "[:=\"\\[\\]]", EndsWithParent: true}
	livescriptM32 = &Mode{Begin: "[A-Za-z$_](?:-[0-9A-Za-z$_]|[0-9A-Za-z$_])*:", End: ":", ReturnEnd: true, ReturnBegin: true, HasRelevance: true, Relevance: 0}
	livescriptM1.Starts = livescriptM2
	livescriptM3.Variants = []*Mode{livescriptM4, livescriptM6, livescriptM7, livescriptM18, livescriptM19}
	livescriptM4.Contains = []*Mode{livescriptM5}
	livescriptM6.Contains = []*Mode{livescriptM5}
	livescriptM7.Contains = []*Mode{livescriptM5, livescriptM8, livescriptM17}
	livescriptM8.Contains = []*Mode{livescriptM0, livescriptM1, livescriptM3, livescriptM9, livescriptM15, livescriptM16}
	livescriptM9.Variants = []*Mode{livescriptM10, livescriptM14}
	livescriptM10.Contains = []*Mode{livescriptM8, livescriptM11}
	livescriptM11.Contains = []*Mode{livescriptM12, livescriptM13}
	livescriptM18.Contains = []*Mode{livescriptM5, livescriptM8, livescriptM17}
	livescriptM20.Contains = []*Mode{livescriptM12, livescriptM21}
	livescriptM23.Variants = []*Mode{livescriptM24, livescriptM28, livescriptM29}
	livescriptM23.Contains = []*Mode{livescriptM25, livescriptM26}
	livescriptM24.Contains = []*Mode{livescriptM25, livescriptM26}
	livescriptM26.Contains = []*Mode{livescriptM27}
	livescriptM27.Contains = []*Mode{Self, livescriptM0, livescriptM1, livescriptM3, livescriptM9, livescriptM15, livescriptM16}
	livescriptM28.Contains = []*Mode{livescriptM25, livescriptM26}
	livescriptM29.Contains = []*Mode{livescriptM25, livescriptM26}
	livescriptM30.Contains = []*Mode{livescriptM31, livescriptM25}
	livescriptM31.Contains = []*Mode{livescriptM25}
	RegisterLanguage("livescript", &Language{
		Name:            "LiveScript",
		Aliases:         []string{"ls"},
		CaseInsensitive: false,
		Keywords:        Keywords{"keyword": "as in of if for while finally var new function do return void else break catch instanceof with throw case default try switch continue typeof delete let yield const class debugger async await static import from export extends then unless until loop of by when and or is isnt not it that otherwise from to til fallthrough case enum native list map __hasProp __extends __slice __bind __indexOf", "literal": "true false null undefined NaN Infinity yes no on off it that void", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}, Illegal: "\\/\\*", Contains: []*Mode{livescriptM0, livescriptM1, livescriptM3, livescriptM9, livescriptM15, livescriptM16, livescriptM20, livescriptM11, livescriptM22, livescriptM23, livescriptM30, livescriptM32},
	})
}
