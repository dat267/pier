package highlight

import "strings"

func init() { RegisterLanguage("rust", Rust()) }

func rustRawString() *Mode {
	// The reference's `r(#*)"(.|\n)*?"\1(?!#)` uses a backreference for the
	// matching count of `#` delimiters, which RE2 has no equivalent for. Expand
	// the plausible counts explicitly and guard the trailing `(?!#)`.
	var alternatives []string
	for n := 0; n <= 4; n++ {
		hashes := strings.Repeat("#", n)
		alternatives = append(alternatives, `r`+hashes+`"(?:.|\n)*?"`+hashes)
	}
	mode := &Mode{
		ClassName: "string",
		Begin:     strings.Join(alternatives, "|"),
	}
	mode.beforeBegin = func(m *match, resp *Response) {
		end := m.index + len(m.lexeme)
		if end < len(m.input) && m.input[end] == '#' {
			resp.IgnoreMatch()
		}
	}
	return mode
}

// Rust returns the Rust language definition (a port of languages/rust.js).
func Rust() *Language {
	const numSuffix = `([ui](8|16|32|64|128|size)|f(32|64))?`
	const keywords = "abstract as async await become box break const continue crate do dyn " +
		"else enum extern false final fn for if impl in let loop macro match mod " +
		"move mut override priv pub ref return self Self static struct super " +
		"trait true try type typeof unsafe unsized use virtual where while yield"
	const builtins = "drop " +
		"i8 i16 i32 i64 i128 isize " +
		"u8 u16 u32 u64 u128 usize " +
		"f32 f64 " +
		"str char bool " +
		"Box Option Result String Vec " +
		"Copy Send Sized Sync Drop Fn FnMut FnOnce ToOwned Clone Debug " +
		"PartialEq PartialOrd Eq Ord AsRef AsMut Into From Default Iterator " +
		"Extend IntoIterator DoubleEndedIterator ExactSizeIterator " +
		"SliceConcatExt ToString " +
		"assert! assert_eq! bitflags! bytes! cfg! col! concat! concat_idents! " +
		"debug_assert! debug_assert_eq! env! panic! file! format! format_args! " +
		"include_bin! include_str! line! local_data_key! module_path! " +
		"option_env! print! println! select! stringify! try! unimplemented! " +
		"unreachable! vec! write! writeln! macro_rules! assert_ne! debug_assert_ne!"

	blockComment := commentMode(`/\*`, `\*/`)
	blockComment.Contains = append(blockComment.Contains, Self)

	titleEndsParent := Variant(UnderscoreTitleMode, func(m *Mode) { m.EndsParent = true })

	numbers := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `\b0b([01_]+)` + numSuffix},
			{ClassName: "number", Begin: `\b0o([0-7_]+)` + numSuffix},
			{ClassName: "number", Begin: `\b0x([A-Fa-f0-9_]+)` + numSuffix},
			{ClassName: "number", Begin: `\b(\d[\d_]*(\.[0-9_]+)?([eE][+-]?[0-9_]+)?)` + numSuffix},
		},
	})

	return &Language{
		Name:    "Rust",
		Aliases: []string{"rs"},
		Keywords: Keywords{
			"$pattern": IdentRe + `!?`,
			"keyword":  keywords,
			"literal":  "true false Some None Ok Err",
			"built_in": builtins,
		},
		Illegal: `</`,
		Contains: []*Mode{
			CLineCommentMode,
			blockComment,
			Variant(QuoteStringMode, func(m *Mode) { m.Begin = `b?"`; m.Illegal = "" }),
			{
				ClassName: "string",
				Variants: []*Mode{
					rustRawString(),
					{ClassName: "string", Begin: `b?'\\?(x\w{2}|u\w{4}|U\w{8}|.)'`},
				},
			},
			{ClassName: "symbol", Begin: `'[a-zA-Z_][a-zA-Z0-9_]*`},
			numbers,
			{
				ClassName:     "function",
				BeginKeywords: "fn",
				End:           `(\(|<)`,
				ExcludeEnd:    true,
				Contains:      []*Mode{UnderscoreTitleMode},
			},
			{
				ClassName: "meta",
				Begin:     `#!?\[`,
				End:       `\]`,
				Contains:  []*Mode{{ClassName: "meta-string", Begin: `"`, End: `"`}},
			},
			{
				ClassName:     "class",
				BeginKeywords: "type",
				End:           `;`,
				Contains:      []*Mode{titleEndsParent},
				Illegal:       `\S`,
			},
			{
				ClassName:     "class",
				BeginKeywords: "trait enum struct union",
				End:           `\{`,
				Contains:      []*Mode{titleEndsParent},
				Illegal:       `[\w\d]`,
			},
			{
				Begin:    IdentRe + `::`,
				Keywords: Keywords{"built_in": builtins},
			},
			{Begin: `->`},
		},
	}
}
