package highlight

var (
	rM0  *Mode
	rM1  *Mode
	rM2  *Mode
	rM3  *Mode
	rM4  *Mode
	rM5  *Mode
	rM6  *Mode
	rM7  *Mode
	rM8  *Mode
	rM9  *Mode
	rM10 *Mode
	rM11 *Mode
	rM12 *Mode
	rM13 *Mode
	rM14 *Mode
	rM15 *Mode
	rM16 *Mode
	rM17 *Mode
	rM18 *Mode
	rM19 *Mode
	rM20 *Mode
	rM21 *Mode
	rM22 *Mode
	rM23 *Mode
	rM24 *Mode
	rM25 *Mode
	rM26 *Mode
	rM27 *Mode
	rM28 *Mode
	rM29 *Mode
	rM30 *Mode
	rM31 *Mode
	rM32 *Mode
	rM33 *Mode
	rM34 *Mode
	rM35 *Mode
)

func init() {
	rM0 = &Mode{ClassName: "comment", Begin: "#'", End: "$"}
	rM1 = &Mode{ClassName: "doctag", Begin: "@examples"}
	rM2 = &Mode{}
	rM3 = &Mode{Begin: "\\n"}
	rM4 = &Mode{Begin: "#'\\s*", BeginFollowedBy: "@[a-zA-Z]+", EndsParent: true}
	rM5 = &Mode{Begin: "#'", End: "$", ExcludeBegin: true}
	rM6 = &Mode{ClassName: "doctag", Begin: "@param", End: "$"}
	rM7 = &Mode{ClassName: "variable", EndsParent: true}
	rM8 = &Mode{ClassName: "variable", Begin: "(?:(?:[a-zA-Z]|\\.[._a-zA-Z])[._a-zA-Z0-9]*)|\\.", BeginNotFollowedBy: "\\d", EndsParent: true}
	rM9 = &Mode{ClassName: "variable", Begin: "`(?:\\\\.|[^`\\\\])+`", EndsParent: true}
	rM10 = &Mode{ClassName: "doctag", Begin: "@[a-zA-Z]+"}
	rM11 = &Mode{ClassName: "meta-keyword", Begin: "\\\\[a-zA-Z]+"}
	rM12 = &Mode{Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b"}
	rM13 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	rM14 = &Mode{ClassName: "comment", Begin: "#", End: "$"}
	rM15 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	rM16 = &Mode{ClassName: "string"}
	rM17 = &Mode{ClassName: "string", Begin: "[rR]\"(-*)\\(", End: "\\)(-*)\""}
	rM18 = &Mode{Begin: "\\\\[\\s\\S]", HasRelevance: true, Relevance: 0}
	rM19 = &Mode{ClassName: "string", Begin: "[rR]\"(-*)\\{", End: "\\}(-*)\""}
	rM20 = &Mode{ClassName: "string", Begin: "[rR]\"(-*)\\[", End: "\\](-*)\""}
	rM21 = &Mode{ClassName: "string", Begin: "[rR]'(-*)\\(", End: "\\)(-*)'"}
	rM22 = &Mode{ClassName: "string", Begin: "[rR]'(-*)\\{", End: "\\}(-*)'"}
	rM23 = &Mode{ClassName: "string", Begin: "[rR]'(-*)\\[", End: "\\](-*)'"}
	rM24 = &Mode{ClassName: "string", Begin: "\"", End: "\"", HasRelevance: true, Relevance: 0}
	rM25 = &Mode{ClassName: "string", Begin: "'", End: "'", HasRelevance: true, Relevance: 0}
	// The reference's beforeMatch extension qualifies the number with a
	// lookahead for one of the variants, so a non-identifier character does not
	// start the mode unless a number follows it.
	rM26 = &Mode{Begin: "([^a-zA-Z0-9._])", BeginFollowedBy: `0[xX][0-9a-fA-F]+\.[0-9a-fA-F]*[pP][+-]?\d+i?|0[xX][0-9a-fA-F]+([pP][+-]?\d+)?[Li]?|(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?[Li]?`, HasRelevance: true, Relevance: 0}
	rM27 = &Mode{HasRelevance: true, Relevance: 0}
	rM28 = &Mode{ClassName: "number", EndsParent: true, HasRelevance: true, Relevance: 0}
	rM29 = &Mode{ClassName: "number", Match: "0[xX][0-9a-fA-F]+\\.[0-9a-fA-F]*[pP][+-]?\\d+i?", EndsParent: true, HasRelevance: true, Relevance: 0}
	rM30 = &Mode{ClassName: "number", Match: "0[xX][0-9a-fA-F]+([pP][+-]?\\d+)?[Li]?", EndsParent: true, HasRelevance: true, Relevance: 0}
	rM31 = &Mode{ClassName: "number", Match: "(\\d+(\\.\\d*)?|\\.\\d+)([eE][+-]?\\d+)?[Li]?", EndsParent: true, HasRelevance: true, Relevance: 0}
	rM32 = &Mode{Begin: "%", End: "%"}
	rM33 = &Mode{Begin: "[a-zA-Z][a-zA-Z_0-9]*\\s+<-\\s+"}
	rM34 = &Mode{Begin: "`", End: "`"}
	rM35 = &Mode{Begin: "\\\\."}
	rM0.Contains = []*Mode{rM1, rM6, rM10, rM11, rM12, rM13}
	rM1.Starts = rM2
	rM2.Contains = []*Mode{rM3, rM4, rM5}
	rM6.Contains = []*Mode{rM7}
	rM7.Variants = []*Mode{rM8, rM9}
	rM14.Contains = []*Mode{rM12, rM15}
	rM16.Variants = []*Mode{rM17, rM19, rM20, rM21, rM22, rM23, rM24, rM25}
	rM16.Contains = []*Mode{rM18}
	rM17.Contains = []*Mode{rM18}
	rM19.Contains = []*Mode{rM18}
	rM20.Contains = []*Mode{rM18}
	rM21.Contains = []*Mode{rM18}
	rM22.Contains = []*Mode{rM18}
	rM23.Contains = []*Mode{rM18}
	rM24.Contains = []*Mode{rM18}
	rM25.Contains = []*Mode{rM18}
	rM26.Starts = rM27
	rM27.Contains = []*Mode{rM28}
	rM28.Variants = []*Mode{rM29, rM30, rM31}
	rM34.Contains = []*Mode{rM35}
	RegisterLanguage("r", &Language{
		Name:            "R",
		Aliases:         []string{},
		CaseInsensitive: false,
		Keywords:        Keywords{"$pattern": "(?:(?:[a-zA-Z]|\\.[._a-zA-Z])[._a-zA-Z0-9]*)|\\.", "keyword": "function if in break next repeat else for while", "literal": "NULL NA TRUE FALSE Inf NaN NA_integer_|10 NA_real_|10 NA_character_|10 NA_complex_|10", "built_in": "LETTERS letters month.abb month.name pi T F abs acos acosh all any anyNA Arg as.call as.character as.complex as.double as.environment as.integer as.logical as.null.default as.numeric as.raw asin asinh atan atanh attr attributes baseenv browser c call ceiling class Conj cos cosh cospi cummax cummin cumprod cumsum digamma dim dimnames emptyenv exp expression floor forceAndCall gamma gc.time globalenv Im interactive invisible is.array is.atomic is.call is.character is.complex is.double is.environment is.expression is.finite is.function is.infinite is.integer is.language is.list is.logical is.matrix is.na is.name is.nan is.null is.numeric is.object is.pairlist is.raw is.recursive is.single is.symbol lazyLoadDBfetch length lgamma list log max min missing Mod names nargs nzchar oldClass on.exit pos.to.env proc.time prod quote range Re rep retracemem return round seq_along seq_len seq.int sign signif sin sinh sinpi sqrt standardGeneric substitute sum switch tan tanh tanpi tracemem trigamma trunc unclass untracemem UseMethod xtfrm"}, Illegal: "->", Contains: []*Mode{rM0, rM14, rM16, rM26, rM32, rM33, rM34},
	})
}
