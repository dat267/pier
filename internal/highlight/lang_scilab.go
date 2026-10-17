package highlight

func init() { RegisterLanguage("scilab", Scilab()) }

// Scilab returns the Scilab language definition (a port of languages/scilab.js).
func Scilab() *Language {
	commonContains := []*Mode{
		CNumberMode,
		{
			ClassName: "string",
			Begin:     `'|"`,
			End:       `'|"`,
			Contains:  []*Mode{BackslashEscape, {Begin: `''`}},
		},
	}
	return &Language{
		Name:    "Scilab",
		Aliases: []string{"sci"},
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "%?\\w+",
				"keyword": "abort break case clear catch continue do elseif else endfunction end for function global if pause " +
					"return resume select try then while",
				"literal": "%f %F %t %T %pi %eps %inf %nan %e %i %z %s",
				"built_in": "abs and acos asin atan ceil cd chdir clearglobal cosh cos cumprod deff disp error exec execstr " +
					"exists exp eye gettext floor fprintf fread fsolve imag isdef isempty isinfisnan isvector lasterror " +
					"length load linspace list listfiles log10 log2 log max min msprintf mclose mopen ones or pathconvert " +
					"poly printf prod pwd rand real round sinh sin size gsort sprintf sqrt strcat strcmps tring sum " +
					"system tanh tan type typename warning zeros matrix",
			},
			Illegal: `("|#|/\*|\s+/\w+)`,
			Contains: append([]*Mode{
				{
					ClassName:     "function",
					BeginKeywords: "function",
					End:           `$`,
					Contains: []*Mode{
						UnderscoreTitleMode,
						{ClassName: "params", Begin: `\(`, End: `\)`},
					},
				},
				{Begin: `[a-zA-Z_][a-zA-Z_0-9]*[\.\']+`, HasRelevance: true, Relevance: 0},
				{
					Begin:        `\[`,
					End:          `\][\.\']*`,
					HasRelevance: true,
					Relevance:    0,
					Contains:     commonContains,
				},
				CommentMode(`//`, `$`),
			}, commonContains...),
		},
	}
}
