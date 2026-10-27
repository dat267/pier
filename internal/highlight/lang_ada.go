package highlight

func init() { RegisterLanguage("ada", Ada()) }

// Ada returns the Ada language definition (a port of languages/ada.js).
func Ada() *Language {
	const integerRe = `\d(_|\d)*`
	const exponentRe = `[eE][-+]?` + integerRe
	const decimalLiteralRe = integerRe + `(\.` + integerRe + `)?` + `(` + exponentRe + `)?`
	const basedIntegerRe = `\w+`
	const basedLiteralRe = integerRe + `#` + basedIntegerRe + `(\.` + basedIntegerRe + `)?` + `#` + `(` + exponentRe + `)?`
	numberRe := `\b(` + basedLiteralRe + `|` + decimalLiteralRe + `)`
	const idRegex = `[A-Za-z](_?[A-Za-z0-9.])*`
	// The reference's BAD_CHARS is `[]\{\}%#'"`, whose `[]` never matches; the
	// effective set is the braces, percent, hash, apostrophe and quote.
	const badChars = `[{}%#'"]`

	comments := CommentMode(`--`, `$`)

	varDecls := &Mode{
		Begin:   `\s+:\s+`,
		End:     `\s*(:=|;|\)|=>|$)`,
		Illegal: badChars,
		Contains: []*Mode{
			{BeginKeywords: "loop for declare others", EndsParent: true},
			{ClassName: "keyword", BeginKeywords: "not null constant access function procedure in out aliased exception"},
			{ClassName: "type", Begin: idRegex, EndsParent: true, HasRelevance: true, Relevance: 0},
		},
	}

	packageMode := &Mode{
		ClassName:    "title",
		Begin:        `(\bwith\s+)?(\bprivate\s+)?\bpackage\s+(\bbody\s+)?`,
		End:          `(is|$)`,
		Keywords:     Keywords{"keyword": "package body"},
		ExcludeBegin: true,
		ExcludeEnd:   true,
		Illegal:      badChars,
	}

	functionMode := &Mode{
		Begin:       `(\b(with|overriding)\s+)?\b(function|procedure)\s+`,
		End:         `(\bis|\bwith|\brenames|\)\s*;)`,
		Keywords:    Keywords{"keyword": "overriding function procedure with is renames return"},
		ReturnBegin: true,
		Contains: []*Mode{
			comments,
			{
				ClassName:    "title",
				Begin:        `(\bwith\s+)?\b(function|procedure)\s+`,
				End:          `(\(|\s+|$)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Illegal:      badChars,
			},
			varDecls,
			{
				ClassName:    "type",
				Begin:        `\breturn\s+`,
				End:          `(\s+|;|$)`,
				Keywords:     Keywords{"keyword": "return"},
				ExcludeBegin: true,
				ExcludeEnd:   true,
				EndsParent:   true,
				Illegal:      badChars,
			},
		},
	}

	typeMode := &Mode{
		ClassName:    "type",
		Begin:        `\b(sub)?type\s+`,
		End:          `\s+`,
		Keywords:     Keywords{"keyword": "type"},
		ExcludeBegin: true,
		Illegal:      badChars,
	}

	return &Language{
		Name:            "Ada",
		CaseInsensitive: true,
		Keywords: Keywords{
			"keyword": "abort else new return abs elsif not reverse abstract end accept entry select access exception of " +
				"separate aliased exit or some all others subtype and for out synchronized array function overriding " +
				"at tagged generic package task begin goto pragma terminate body private then if procedure type case " +
				"in protected constant interface is raise use declare range delay limited record when delta loop rem " +
				"while digits renames with do mod requeue xor",
			"literal": "True False",
		},
		Contains: []*Mode{
			comments,
			{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{{Begin: `""`, HasRelevance: true, Relevance: 0}}},
			{ClassName: "string", Begin: `'.'`},
			{ClassName: "number", Begin: numberRe, HasRelevance: true, Relevance: 0},
			{ClassName: "symbol", Begin: "'" + idRegex},
			packageMode,
			functionMode,
			typeMode,
			varDecls,
		},
	}
}
