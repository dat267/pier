package highlight

func init() { RegisterLanguage("dos", DOS()) }

// DOS returns the Batch file (DOS) language definition (a port of
// languages/dos.js). The label rule doubles as the function rule; its title is
// the reference's TITLE_MODE with a widened begin.
func DOS() *Language {
	comment := CommentMode(`^\s*@?rem\b`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 10

	const labelBegin = `^\s*[A-Za-z._?][A-Za-z0-9_$#@~.?]*(:|\s+label)`

	title := Variant(TitleMode, func(m *Mode) {
		m.Begin = `([_a-zA-Z]\w*\.)*([_a-zA-Z]\w*:)?[_a-zA-Z]\w*`
	})

	return &Language{
		Name:            "Batch file (DOS)",
		Aliases:         []string{"bat", "cmd"},
		CaseInsensitive: true,
		Illegal:         `\/\*`,
		Keywords: Keywords{
			"keyword": "if else goto for in do call exit not exist errorlevel defined " +
				"equ neq lss leq gtr geq",
			"built_in": "prn nul lpt3 lpt2 lpt1 con com4 com3 com2 com1 aux " +
				"shift cd dir echo setlocal endlocal set pause copy " +
				"append assoc at attrib break cacls cd chcp chdir chkdsk chkntfs cls cmd color " +
				"comp compact convert date dir diskcomp diskcopy doskey erase fs " +
				"find findstr format ftype graftabl help keyb label md mkdir mode more move path " +
				"pause print popd pushd promt rd recover rem rename replace restore rmdir shift " +
				"sort start subst time title tree type ver verify vol " +
				"ping net ipconfig taskkill xcopy ren del",
		},
		Contains: []*Mode{
			{ClassName: "variable", Begin: `%%[^ ]|%[^ ]+?%|![^ ]+?!`},
			{
				ClassName: "function",
				Begin:     labelBegin,
				End:       `goto:eof`,
				Contains:  []*Mode{title, comment},
			},
			{ClassName: "number", Begin: `\b\d+`, HasRelevance: true, Relevance: 0},
			comment,
		},
	}
}
