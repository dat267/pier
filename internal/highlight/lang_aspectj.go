package highlight

func init() { RegisterLanguage("aspectj", AspectJ()) }

// AspectJ returns the AspectJ language definition (a port of
// languages/aspectj.js).
func AspectJ() *Language {
	const keywords = "false synchronized int abstract float private char boolean static null if const " +
		"for true while long throw strictfp finally protected import native final return void " +
		"enum else extends implements break transient new catch instanceof byte super volatile case " +
		"assert short package default double public try this switch continue throws privileged " +
		"aspectOf adviceexecution proceed cflowbelow cflow initialization preinitialization " +
		"staticinitialization withincode target within execution getWithinTypeName handler " +
		"thisJoinPoint thisJoinPointStaticPart thisEnclosingJoinPointStaticPart declare parents " +
		"warning error soft precedence thisAspectInstance"
	const shortkeys = "get set args call"

	docComment := &Mode{
		ClassName:    "comment",
		Begin:        `/\*\*`,
		End:          `\*/`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{Begin: `\w+@`, HasRelevance: true, Relevance: 0},
			{ClassName: "doctag", Begin: `@[A-Za-z]+`},
		},
	}

	aspectClass := &Mode{
		ClassName:     "class",
		BeginKeywords: "aspect",
		End:           `[{;=]`,
		ExcludeEnd:    true,
		Illegal:       `[:;"\[\]]`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements pertypewithin perthis pertarget percflowbelow percflow issingleton"},
			UnderscoreTitleMode,
			{
				Begin:      `\([^\)]*`,
				End:        `[)]+`,
				Keywords:   Keywords{"keyword": keywords + " " + shortkeys},
				ExcludeEnd: false,
			},
		},
	}

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface",
		End:           `[{;=]`,
		ExcludeEnd:    true,
		HasRelevance:  true,
		Relevance:     0,
		Keywords:      Keywords{"keyword": "class interface"},
		Illegal:       `[:"\[\]]`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}

	pointcut := &Mode{
		BeginKeywords: "pointcut after before around throwing returning",
		End:           `[)]`,
		ExcludeEnd:    false,
		Illegal:       `["\[\]]`,
		Contains: []*Mode{
			{
				Begin:       UnderscoreIdentRe + `\s*\(`,
				ReturnBegin: true,
				Contains:    []*Mode{UnderscoreTitleMode},
			},
		},
	}

	colon := &Mode{
		Begin:        `[:]`,
		ReturnBegin:  true,
		End:          `[{;]`,
		HasRelevance: true,
		Relevance:    0,
		ExcludeEnd:   false,
		Keywords:     Keywords{"keyword": keywords},
		Illegal:      `["\[\]]`,
		Contains: []*Mode{
			{
				Begin:        UnderscoreIdentRe + `\s*\(`,
				Keywords:     Keywords{"keyword": keywords + " " + shortkeys},
				HasRelevance: true,
				Relevance:    0,
			},
			QuoteStringMode,
		},
	}

	function := &Mode{
		ClassName:   "function",
		Begin:       `\w+ +\w+(\.\w+)?\s*\([^\)]*\)\s*((throws)[\w\s,]+)?[\{;]`,
		ReturnBegin: true,
		End:         `[{;=]`,
		Keywords:    Keywords{"keyword": keywords},
		ExcludeEnd:  true,
		Contains: []*Mode{
			{
				Begin:        UnderscoreIdentRe + `\s*\(`,
				ReturnBegin:  true,
				HasRelevance: true,
				Relevance:    0,
				Contains:     []*Mode{UnderscoreTitleMode},
			},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				HasRelevance: true,
				Relevance:    0,
				Keywords:     Keywords{"keyword": keywords},
				Contains: []*Mode{
					AposStringMode,
					QuoteStringMode,
					CNumberMode,
					CBlockCommentMode,
				},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	return &Language{
		Name: "AspectJ",
		Mode: Mode{
			Keywords: Keywords{"keyword": keywords},
			Illegal:  `<\/|#`,
			Contains: []*Mode{
				docComment,
				CLineCommentMode,
				CBlockCommentMode,
				AposStringMode,
				QuoteStringMode,
				aspectClass,
				classMode,
				pointcut,
				colon,
				{BeginKeywords: "new throw", HasRelevance: true, Relevance: 0},
				function,
				CNumberMode,
				{ClassName: "meta", Begin: `@[A-Za-z]+`},
			},
		},
	}
}
