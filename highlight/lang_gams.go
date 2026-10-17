package highlight

func init() { RegisterLanguage("gams", Gams()) }

// Gams returns the GAMS language definition (a port of languages/gams.js).
func Gams() *Language {
	keywords := Keywords{
		"keyword": "abort acronym acronyms alias all and assign binary card diag display else eq file files for free ge " +
			"gt if integer le loop lt maximizing minimizing model models ne negative no not option options or ord " +
			"positive prod put putpage puttl repeat sameas semicont semiint smax smin solve sos1 sos2 sum system " +
			"table then until using while xor yes",
		"literal": "eps inf na",
		"built_in": "abs arccos arcsin arctan arctan2 Beta betaReg binomial ceil centropy cos cosh cvPower div div0 eDist " +
			"entropy errorf execSeed exp fact floor frac gamma gammaReg log logBeta logGamma log10 log2 mapVal " +
			"max min mod ncpCM ncpF ncpVUpow ncpVUsin normal pi poly power randBinomial randLinear randTriangle " +
			"round rPower sigmoid sign signPower sin sinh slexp sllog10 slrec sqexp sqlog10 sqr sqrec sqrt tan " +
			"tanh trunc uniform uniformInt vcPower bool_and bool_eqv bool_imp bool_not bool_or bool_xor ifThen " +
			"rel_eq rel_ge rel_gt rel_le rel_lt rel_ne gday gdow ghour gleap gmillisec gminute gmonth gsecond " +
			"gyear jdate jnow jstart jtime errorLevel execError gamsRelease gamsVersion handleCollect " +
			"handleDelete handleStatus handleSubmit heapFree heapLimit heapSize jobHandle jobKill jobStatus " +
			"jobTerminate licenseLevel licenseStatus maxExecError sleep timeClose timeComp timeElapsed timeExec " +
			"timeStart",
	}

	params := &Mode{
		ClassName:    "params",
		Begin:        `\(`,
		End:          `\)`,
		ExcludeBegin: true,
		ExcludeEnd:   true,
	}
	symbols := &Mode{ClassName: "symbol"}
	symbols.Variants = []*Mode{
		Variant(symbols, func(m *Mode) { m.Begin = `=[lgenxc]=` }),
		Variant(symbols, func(m *Mode) { m.Begin = `\$` }),
	}

	qstr := &Mode{ClassName: "comment", Illegal: `\n`, Contains: []*Mode{BackslashEscape}}
	qstr.Variants = []*Mode{
		Variant(qstr, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(qstr, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
	}

	assignment := &Mode{
		Begin:    `/`,
		End:      `/`,
		Keywords: keywords,
		Contains: []*Mode{
			qstr,
			CLineCommentMode,
			CBlockCommentMode,
			QuoteStringMode,
			AposStringMode,
			CNumberMode,
		},
	}

	const commentWord = `[a-z0-9&#*=?@\\><:,()$[\]_.{}!+%^-]+`
	descText := &Mode{
		Begin:          `[a-z][a-z0-9_]*(\([a-z0-9_, ]*\))?[ \t]+`,
		ExcludeBegin:   true,
		End:            `$`,
		EndsWithParent: true,
		Contains: []*Mode{
			qstr,
			assignment,
			{
				ClassName:    "comment",
				Begin:        commentWord + `([ ]+` + commentWord + `)*`,
				HasRelevance: true,
				Relevance:    0,
			},
		},
	}

	declarations := &Mode{
		BeginKeywords: "set sets parameter parameters variable variables scalar scalars equation equations",
		End:           `;`,
		Contains: []*Mode{
			CommentMode(`^\*`, `$`),
			CLineCommentMode,
			CBlockCommentMode,
			QuoteStringMode,
			AposStringMode,
			assignment,
			descText,
		},
	}

	table := &Mode{
		BeginKeywords: "table",
		End:           `;`,
		ReturnBegin:   true,
		Contains: []*Mode{
			{
				BeginKeywords: "table",
				End:           `$`,
				Contains:      []*Mode{descText},
			},
			CommentMode(`^\*`, `$`),
			CLineCommentMode,
			CBlockCommentMode,
			QuoteStringMode,
			AposStringMode,
			CNumberMode,
		},
	}

	function := &Mode{
		ClassName:   "function",
		Begin:       `^[a-z][a-z0-9_,\-+' ()$]+\.{2}`,
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "title", Begin: `^[a-z0-9_]+`},
			params,
			symbols,
		},
	}

	return &Language{
		Name:            "GAMS",
		Aliases:         []string{"gms"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: keywords,
			Contains: []*Mode{
				CommentMode(`^\$ontext`, `^\$offtext`),
				{
					ClassName:   "meta",
					Begin:       `^\$[a-z0-9]+`,
					End:         `$`,
					ReturnBegin: true,
					Contains:    []*Mode{{ClassName: "meta-keyword", Begin: `^\$[a-z0-9]+`}},
				},
				CommentMode(`^\*`, `$`),
				CLineCommentMode,
				CBlockCommentMode,
				QuoteStringMode,
				AposStringMode,
				declarations,
				table,
				function,
				CNumberMode,
				symbols,
			},
		},
	}
}
