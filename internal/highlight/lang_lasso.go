package highlight

import "strings"

func init() { RegisterLanguage("lasso", Lasso()) }

const (
	lassoIdentRe = `[a-zA-Z_][\w.]*`
	lassoAngleRe = `<\?(lasso(script)?|=)`
	lassoCloseRe = `\]|\?>`
)

// lassoNotInfinity guards the reference's `-(?!infinity)`.
func lassoNotInfinity(m *match) bool {
	return !strings.HasPrefix(m.input[m.index+1:], "infinity")
}

// lassoTitleEnd matches the define title's
// `IDENT(=(?!>))?|[-+*/%](?!>)`: an identifier with an optional `=` that is not
// followed by `>`, or one operator not followed by `>`.
func lassoTitleEnd(s string, pos int) (int, bool) {
	if pos >= len(s) {
		return 0, false
	}
	c := s[pos]
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
		i := pos + 1
		for i < len(s) && (isWordByte(s[i]) || s[i] == '.') {
			i++
		}
		if i < len(s) && s[i] == '=' && !(i+1 < len(s) && s[i+1] == '>') {
			i++
		}
		return i, true
	}
	if strings.IndexByte("-+*/%", c) >= 0 {
		if pos+1 < len(s) && s[pos+1] == '>' {
			return 0, false
		}
		return pos + 1, true
	}
	return 0, false
}

const lassoTitleFirsts = "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-+*/%"

// Lasso returns the Lasso language definition (a port of languages/lasso.js).
func Lasso() *Language {
	keywords := Keywords{
		"$pattern": "[a-zA-Z_][\\w.]*|&[lg]t;",
		"literal":  "true false none minimal full all void and or not bw nbw ew new cn ncn lt lte gt gte eq neq rx nrx ft",
		"built_in": "array date decimal duration integer map pair string tag xml null boolean bytes keyword list locale " +
			"queue set stack staticarray local var variable global data self inherited currentcapture givenblock",
		"keyword": "cache database_names database_schemanames database_tablenames define_tag define_type email_batch " +
			"encode_set html_comment handle handle_error header if inline iterate ljax_target link " +
			"link_currentaction link_currentgroup link_currentrecord link_detail link_firstgroup link_firstrecord " +
			"link_lastgroup link_lastrecord link_nextgroup link_nextrecord link_prevgroup link_prevrecord log " +
			"loop namespace_using output_none portal private protect records referer referrer repeating resultset " +
			"rows search_args search_arguments select sort_args sort_arguments thread_atomic value_list while " +
			"abort case else fail_if fail_ifnot fail if_empty if_false if_null if_true loop_abort loop_continue " +
			"loop_count params params_up return return_value run_children soap_definetag soap_lastrequest " +
			"soap_lastresponse tag_name ascending average by define descending do equals frozen group " +
			"handle_failure import in into join let match max min on order parent protected provide public " +
			"require returnhome skip split_thread sum take thread to trait type where with yield yieldhome",
	}

	htmlComment := CommentMode(`<!--`, `-->`)
	htmlComment.HasRelevance = true
	htmlComment.Relevance = 0

	noProcess := &Mode{
		ClassName: "meta",
		Begin:     `\[noprocess\]`,
		Starts:    &Mode{End: `\[/noprocess\]`, ReturnEnd: true, Contains: []*Mode{htmlComment}},
	}
	startMode := &Mode{ClassName: "meta", Begin: `\[/noprocess|` + lassoAngleRe}
	dataMember := &Mode{ClassName: "symbol", Begin: `'` + lassoIdentRe + `'`}

	numberMode := Variant(CNumberMode, func(m *Mode) { m.Begin = CNumberRe + `|(-?infinity|NaN)\b` })
	apos := Variant(AposStringMode, func(m *Mode) { m.Illegal = "" })
	quote := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })

	variables := &Mode{}
	variables.Variants = []*Mode{
		{Begin: `[#$]` + lassoIdentRe},
		{Begin: `#`, End: `\d+`, Illegal: `\W`},
	}

	paramsBase := &Mode{ClassName: "params"}
	paramsBase.Variants = []*Mode{
		Variant(paramsBase, func(m *Mode) { m.Begin = `-` + lassoIdentRe; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(paramsBase, func(m *Mode) { m.Begin = `(\.\.\.)` }),
	}
	for _, v := range paramsBase.Variants {
		v.beforeBegin = func(m *match, resp *Response) {
			if !lassoNotInfinity(m) {
				resp.IgnoreMatch()
			}
		}
	}

	memberAccess := &Mode{
		Begin:        `(->|\.)\s*`,
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{dataMember},
	}

	title := &Mode{
		ClassName:    "title",
		ManualBegin:  lassoTitleEnd,
		ManualFirst:  lassoTitleFirsts,
		HasRelevance: true,
		Relevance:    0,
	}
	define := &Mode{
		ClassName:     "class",
		BeginKeywords: "define",
		ReturnEnd:     true,
		End:           `\(|=>`,
		Contains:      []*Mode{title},
	}

	code := []*Mode{
		CLineCommentMode,
		CBlockCommentMode,
		numberMode,
		apos,
		quote,
		{ClassName: "string", Begin: "\x60", End: "\x60"},
		variables,
		{ClassName: "type", Begin: `::\s*`, End: lassoIdentRe, Illegal: `\W`},
		paramsBase,
		memberAccess,
		define,
	}

	noSquareStarts := &Mode{
		End:      `\[/no_square_brackets\]`,
		Keywords: keywords,
		Contains: append([]*Mode{
			{
				ClassName:    "meta",
				Begin:        lassoCloseRe,
				HasRelevance: true,
				Relevance:    0,
				Starts:       &Mode{End: `\[noprocess\]|` + lassoAngleRe, ReturnEnd: true, Contains: []*Mode{htmlComment}},
			},
			noProcess,
			startMode,
		}, code...),
	}

	contains := []*Mode{
		{
			ClassName:    "meta",
			Begin:        lassoCloseRe,
			HasRelevance: true,
			Relevance:    0,
			Starts:       &Mode{End: `\[|` + lassoAngleRe, ReturnEnd: true, HasRelevance: true, Relevance: 0, Contains: []*Mode{htmlComment}},
		},
		noProcess,
		startMode,
		{ClassName: "meta", Begin: `\[no_square_brackets`, Starts: noSquareStarts},
		{ClassName: "meta", Begin: `\[`, HasRelevance: true, Relevance: 0},
		{ClassName: "meta", Begin: `^#!`, End: `lasso9$`, HasRelevance: true, Relevance: 10},
	}
	contains = append(contains, code...)

	return &Language{
		Name:            "Lasso",
		Aliases:         []string{"ls", "lassoscript"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: keywords,
			Contains: contains,
		},
	}
}
