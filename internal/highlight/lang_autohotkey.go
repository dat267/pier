package highlight

func init() { RegisterLanguage("autohotkey", AutoHotkey()) }

// AutoHotkey returns the AutoHotkey language definition (a port of
// languages/autohotkey.js). The title variants' `(?!=)` lookaheads become
// beforeBegin guards.
func AutoHotkey() *Language {
	backtick := &Mode{Begin: "\x60[\\s\\S]"}
	str := Variant(QuoteStringMode, func(m *Mode) { m.Contains = []*Mode{backtick} })

	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0

	titleBase := &Mode{ClassName: "title"}
	doubleColon := Variant(titleBase, func(m *Mode) { m.Begin = `^[^\n";]+::` })
	doubleColon.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '=' {
			resp.IgnoreMatch()
		}
	}
	singleColon := Variant(titleBase, func(m *Mode) {
		m.Begin = `^[^\n";]+:`
		m.HasRelevance = true
		m.Relevance = 0
	})
	singleColon.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '=' {
			resp.IgnoreMatch()
		}
	}
	titleBase.Variants = []*Mode{doubleColon, singleColon}

	return &Language{
		Name:            "AutoHotkey",
		Aliases:         []string{"ahk"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"keyword":  "Break Continue Critical Exit ExitApp Gosub Goto New OnExit Pause return SetBatchLines SetTimer Suspend Thread Throw Until ahk_id ahk_class ahk_pid ahk_exe ahk_group",
				"literal":  "true false NOT AND OR",
				"built_in": "ComSpec Clipboard ClipboardAll ErrorLevel",
			},
			Contains: []*Mode{
				backtick,
				str,
				comment,
				CBlockCommentMode,
				{ClassName: "number", Begin: NumberRe, HasRelevance: true, Relevance: 0},
				{ClassName: "variable", Begin: `%[a-zA-Z0-9#_$@]+%`},
				{ClassName: "built_in", Begin: `^\s*\w+\s*(,|%)`},
				titleBase,
				{ClassName: "meta", Begin: `^\s*#\w+`, End: `$`, HasRelevance: true, Relevance: 0},
				{ClassName: "built_in", Begin: `A_[a-zA-Z0-9]+`},
				{Begin: `,\s*,`},
			},
		},
	}
}
