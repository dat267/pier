package highlight

func init() { RegisterLanguage("d", D()) }

// D returns the D language definition (a port of languages/d.js).
func D() *Language {
	const decimalIntegerRe = `(0|[1-9][\d_]*)`
	const decimalIntegerNoSusRe = `(0|[1-9][\d_]*|\d[\d_]*|[\d_]+?\d)`
	const binaryIntegerRe = `0[bB][01_]+`
	const hexDigitsRe = `([\da-fA-F][\da-fA-F_]*|_[\da-fA-F][\da-fA-F_]*)`
	const hexIntegerRe = `0[xX]` + hexDigitsRe
	const decimalExponentRe = `([eE][+-]?` + decimalIntegerNoSusRe + `)`
	const decimalFloatRe = `(` + decimalIntegerNoSusRe + `(\.\d*|` + decimalExponentRe + `)|` +
		`\d+\.` + decimalIntegerNoSusRe + `|` +
		`\.` + decimalIntegerRe + decimalExponentRe + `?` +
		`)`
	const hexFloatRe = `(0[xX](` +
		hexDigitsRe + `\.` + hexDigitsRe + `|` +
		`\.?` + hexDigitsRe +
		`)[pP][+-]?` + decimalIntegerNoSusRe + `)`
	const integerRe = `(` + decimalIntegerRe + `|` + binaryIntegerRe + `|` + hexIntegerRe + `)`
	const floatRe = `(` + hexFloatRe + `|` + decimalFloatRe + `)`
	const escapeSequenceRe = `\\(` +
		`['"\?\\abfnrtv]|` +
		`u[\dA-Fa-f]{4}|` +
		`[0-7]{1,3}|` +
		`x[\dA-Fa-f]{2}|` +
		`U[\dA-Fa-f]{8}` +
		`)|` +
		`&[a-zA-Z\d]{2,};`

	integerMode := &Mode{
		ClassName:    "number",
		Begin:        `\b` + integerRe + `(L|u|U|Lu|LU|uL|UL)?`,
		HasRelevance: true,
		Relevance:    0,
	}
	floatMode := &Mode{
		ClassName: "number",
		Begin: `\b(` +
			floatRe + `([fF]|L|i|[fF]i|Li)?|` +
			integerRe + `(i|[fF]i|Li)` +
			`)`,
		HasRelevance: true,
		Relevance:    0,
	}
	characterMode := &Mode{
		ClassName: "string",
		Begin:     `'(` + escapeSequenceRe + `|.)`,
		End:       `'`,
		Illegal:   `.`,
	}
	escapeSequence := &Mode{Begin: escapeSequenceRe, HasRelevance: true, Relevance: 0}

	stringMode := &Mode{ClassName: "string", Begin: `"`, Contains: []*Mode{escapeSequence}, End: `"[cwd]?`}
	wysiwygDelimited := &Mode{ClassName: "string", Begin: `[rq]"`, End: `"[cwd]?`, HasRelevance: true, Relevance: 5}
	alternateWysiwyg := &Mode{ClassName: "string", Begin: "\x60", End: "\x60[cwd]?"}
	hexString := &Mode{ClassName: "string", Begin: `x"[\da-fA-F\s\n\r]*"[cwd]?`, HasRelevance: true, Relevance: 10}
	tokenString := &Mode{ClassName: "string", Begin: `q"\{`, End: `\}"`}
	hashbang := &Mode{ClassName: "meta", Begin: `^#!`, End: `$`, HasRelevance: true, Relevance: 5}
	specialToken := &Mode{ClassName: "meta", Begin: `#(line)`, End: `$`, HasRelevance: true, Relevance: 5}
	attribute := &Mode{ClassName: "keyword", Begin: `@[a-zA-Z_][a-zA-Z_\d]*`}

	nestingComment := CommentMode(`\/\+`, `\+\/`)
	nestingComment.Contains = append([]*Mode{Self}, nestingComment.Contains...)
	nestingComment.HasRelevance = true
	nestingComment.Relevance = 10

	return &Language{
		Name: "D",
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "[a-zA-Z_]\\w*",
				"keyword": "abstract alias align asm assert auto body break byte case cast catch class const continue debug " +
					"default delete deprecated do else enum export extern final finally for foreach foreach_reverse|10 " +
					"goto if immutable import in inout int interface invariant is lazy macro mixin module new nothrow out " +
					"override package pragma private protected public pure ref return scope shared static struct super " +
					"switch synchronized template this throw try typedef typeid typeof union unittest version void " +
					"volatile while with __FILE__ __LINE__ __gshared|10 __thread __traits __DATE__ __EOF__ __TIME__ " +
					"__TIMESTAMP__ __VENDOR__ __VERSION__",
				"built_in": "bool cdouble cent cfloat char creal dchar delegate double dstring float function idouble ifloat " +
					"ireal long real short string ubyte ucent uint ulong ushort wchar wstring",
				"literal": "false null true",
			},
			Contains: []*Mode{
				CLineCommentMode,
				CBlockCommentMode,
				nestingComment,
				hexString,
				stringMode,
				wysiwygDelimited,
				alternateWysiwyg,
				tokenString,
				floatMode,
				integerMode,
				characterMode,
				hashbang,
				specialToken,
				attribute,
			},
		},
	}
}
