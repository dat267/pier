package highlight

// coffeeScriptRegexpEnd matches the reference's regexp literal
// `\/(?![ *]).*?(?![\\]).\/[gim]{0,3}(?=\W)`: a slash, a non-space/non-star
// body, a final body char that is not a backslash, then a slash and flags
// followed by a non-word char. The lazy body backtracks, which a mode-with-end
// cannot express.
func coffeeScriptRegexpEnd(s string, pos int) (int, bool) {
	if pos >= len(s) || s[pos] != '/' {
		return 0, false
	}
	if pos+1 >= len(s) || s[pos+1] == ' ' || s[pos+1] == '*' {
		return 0, false
	}
	for i := pos + 1; i < len(s); i++ {
		if s[i] == '\n' {
			return 0, false
		}
		if s[i] != '\\' && i+1 < len(s) && s[i+1] == '/' {
			j := i + 2
			for j < len(s) && (s[j] == 'g' || s[j] == 'i' || s[j] == 'm') {
				j++
			}
			if j < len(s) && !isWordByte(s[j]) {
				return j, true
			}
		}
	}
	return 0, false
}

var (
	coffeescriptM0  *Mode
	coffeescriptM1  *Mode
	coffeescriptM2  *Mode
	coffeescriptM3  *Mode
	coffeescriptM4  *Mode
	coffeescriptM5  *Mode
	coffeescriptM6  *Mode
	coffeescriptM7  *Mode
	coffeescriptM8  *Mode
	coffeescriptM9  *Mode
	coffeescriptM10 *Mode
	coffeescriptM11 *Mode
	coffeescriptM12 *Mode
	coffeescriptM13 *Mode
	coffeescriptM14 *Mode
	coffeescriptM15 *Mode
	coffeescriptM16 *Mode
	coffeescriptM17 *Mode
	coffeescriptM18 *Mode
	coffeescriptM19 *Mode
	coffeescriptM20 *Mode
	coffeescriptM21 *Mode
	coffeescriptM22 *Mode
	coffeescriptM23 *Mode
	coffeescriptM24 *Mode
	coffeescriptM25 *Mode
	coffeescriptM26 *Mode
	coffeescriptM27 *Mode
	coffeescriptM28 *Mode
	coffeescriptM29 *Mode
	coffeescriptM30 *Mode
	coffeescriptM31 *Mode
)

func init() {
	coffeescriptM0 = &Mode{ClassName: "number", Begin: "\\b(0b[01]+)", HasRelevance: true, Relevance: 0}
	coffeescriptM1 = &Mode{ClassName: "number", Begin: "(-?)(\\b0[xX][a-fA-F0-9]+|(\\b\\d+(\\.\\d*)?|\\.\\d+)([eE][-+]?\\d+)?)", HasRelevance: true, Relevance: 0}
	coffeescriptM2 = &Mode{End: "(\\s*/)?", HasRelevance: true, Relevance: 0}
	coffeescriptM3 = &Mode{ClassName: "string"}
	coffeescriptM4 = &Mode{ClassName: "string", Begin: "'''", End: "'''"}
	coffeescriptM5 = &Mode{Begin: "\\\\[\\s\\S]", HasRelevance: true, Relevance: 0}
	coffeescriptM6 = &Mode{ClassName: "string", Begin: "'", End: "'"}
	coffeescriptM7 = &Mode{ClassName: "string", Begin: "\"\"\"", End: "\"\"\""}
	coffeescriptM8 = &Mode{ClassName: "subst", Begin: "#\\{", End: "\\}", Keywords: Keywords{"keyword": "as in of if for while finally new do return void else break catch instanceof with throw case default try switch continue typeof delete yield class debugger async await import from export extends then unless until loop by when and or is isnt not", "literal": "true false null undefined NaN Infinity yes no on off", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}}
	coffeescriptM9 = &Mode{ClassName: "regexp"}
	coffeescriptM10 = &Mode{ClassName: "regexp", Begin: "///", End: "///"}
	coffeescriptM11 = &Mode{ClassName: "comment", Begin: "#", End: "$"}
	coffeescriptM12 = &Mode{Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b"}
	coffeescriptM13 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	coffeescriptM14 = &Mode{ClassName: "regexp", Begin: "//[gim]{0,3}", BeginFollowedBy: "\\W", HasRelevance: true, Relevance: 0}
	coffeescriptM15 = &Mode{ClassName: "regexp", ManualBegin: coffeeScriptRegexpEnd, ManualFirst: "/"}
	coffeescriptM16 = &Mode{Begin: "@[A-Za-z$_][0-9A-Za-z$_]*"}
	coffeescriptM17 = &Mode{ExcludeBegin: true, ExcludeEnd: true, SubLanguage: []string{"javascript"}}
	coffeescriptM18 = &Mode{Begin: "```", End: "```", ExcludeBegin: true, ExcludeEnd: true, SubLanguage: []string{"javascript"}}
	coffeescriptM19 = &Mode{Begin: "`", End: "`", ExcludeBegin: true, ExcludeEnd: true, SubLanguage: []string{"javascript"}}
	coffeescriptM20 = &Mode{ClassName: "string", Begin: "\"", End: "\""}
	coffeescriptM21 = &Mode{ClassName: "comment", Begin: "###", End: "###"}
	coffeescriptM22 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	coffeescriptM23 = &Mode{ClassName: "function", Begin: "^\\s*[A-Za-z$_][0-9A-Za-z$_]*\\s*=\\s*(\\(.*\\)\\s*)?\\B[-=]>", End: "[-=]>", ReturnBegin: true}
	coffeescriptM24 = &Mode{ClassName: "title", Begin: "[A-Za-z$_][0-9A-Za-z$_]*", HasRelevance: true, Relevance: 0}
	coffeescriptM25 = &Mode{ClassName: "params", Begin: "\\([^\\(]", ReturnBegin: true}
	coffeescriptM26 = &Mode{Begin: "\\(", End: "\\)", Keywords: Keywords{"keyword": "as in of if for while finally new do return void else break catch instanceof with throw case default try switch continue typeof delete yield class debugger async await import from export extends then unless until loop by when and or is isnt not", "literal": "true false null undefined NaN Infinity yes no on off", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}}
	coffeescriptM27 = &Mode{Begin: "[:\\(,=]\\s*", HasRelevance: true, Relevance: 0}
	coffeescriptM28 = &Mode{ClassName: "function", Begin: "(\\(.*\\)\\s*)?\\B[-=]>", End: "[-=]>", ReturnBegin: true}
	coffeescriptM29 = &Mode{ClassName: "class", BeginKeywords: "class", Illegal: "[:=\"\\[\\]]", End: "$"}
	coffeescriptM30 = &Mode{BeginKeywords: "extends", Illegal: "[:=\"\\[\\]]", EndsWithParent: true}
	coffeescriptM31 = &Mode{Begin: "[A-Za-z$_][0-9A-Za-z$_]*:", End: ":", ReturnEnd: true, ReturnBegin: true, HasRelevance: true, Relevance: 0}
	coffeescriptM1.Starts = coffeescriptM2
	coffeescriptM3.Variants = []*Mode{coffeescriptM4, coffeescriptM6, coffeescriptM7, coffeescriptM20}
	coffeescriptM4.Contains = []*Mode{coffeescriptM5}
	coffeescriptM6.Contains = []*Mode{coffeescriptM5}
	coffeescriptM7.Contains = []*Mode{coffeescriptM5, coffeescriptM8}
	coffeescriptM8.Contains = []*Mode{coffeescriptM0, coffeescriptM1, coffeescriptM3, coffeescriptM9, coffeescriptM16, coffeescriptM17}
	coffeescriptM9.Variants = []*Mode{coffeescriptM10, coffeescriptM14, coffeescriptM15}
	coffeescriptM10.Contains = []*Mode{coffeescriptM8, coffeescriptM11}
	coffeescriptM11.Contains = []*Mode{coffeescriptM12, coffeescriptM13}
	coffeescriptM17.Variants = []*Mode{coffeescriptM18, coffeescriptM19}
	coffeescriptM20.Contains = []*Mode{coffeescriptM5, coffeescriptM8}
	coffeescriptM21.Contains = []*Mode{coffeescriptM12, coffeescriptM22}
	coffeescriptM23.Contains = []*Mode{coffeescriptM24, coffeescriptM25}
	coffeescriptM25.Contains = []*Mode{coffeescriptM26}
	coffeescriptM26.Contains = []*Mode{Self, coffeescriptM0, coffeescriptM1, coffeescriptM3, coffeescriptM9, coffeescriptM16, coffeescriptM17}
	coffeescriptM27.Contains = []*Mode{coffeescriptM28}
	coffeescriptM28.Contains = []*Mode{coffeescriptM25}
	coffeescriptM29.Contains = []*Mode{coffeescriptM30, coffeescriptM24}
	coffeescriptM30.Contains = []*Mode{coffeescriptM24}
	RegisterLanguage("coffeescript", &Language{
		Name:            "CoffeeScript",
		Aliases:         []string{"coffee", "cson", "iced"},
		CaseInsensitive: false,
		Keywords:        Keywords{"keyword": "as in of if for while finally new do return void else break catch instanceof with throw case default try switch continue typeof delete yield class debugger async await import from export extends then unless until loop by when and or is isnt not", "literal": "true false null undefined NaN Infinity yes no on off", "built_in": "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape arguments this super console window document localStorage module global Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError npm print"}, Illegal: "\\/\\*", Contains: []*Mode{coffeescriptM0, coffeescriptM1, coffeescriptM3, coffeescriptM9, coffeescriptM16, coffeescriptM17, coffeescriptM21, coffeescriptM11, coffeescriptM23, coffeescriptM27, coffeescriptM29, coffeescriptM31},
	})
}
