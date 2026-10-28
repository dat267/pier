package highlight

import (
	"strings"
	"unicode/utf8"
)

func init() { RegisterLanguage("autohotkey", AutoHotkey()) }

// ahkTitleFirsts is every byte `[^\n";]` can start with.
var ahkTitleFirsts = func() string {
	var b strings.Builder
	for c := 1; c < 256; c++ {
		if c != '\n' && c != '"' && c != ';' {
			b.WriteByte(byte(c))
		}
	}
	return b.String()
}()

// ahkTitleEnd ports `^[^\n";]+<separator>(?!=)`, backtracking to an earlier
// separator when the last is followed by "=".
func ahkTitleEnd(separator string) func(string, int) (int, bool) {
	return func(s string, pos int) (int, bool) {
		if pos > 0 {
			previous, _ := utf8.DecodeLastRuneInString(s[:pos])
			if !jsLineEnd(previous) {
				return 0, false
			}
		}
		end := pos
		for end < len(s) && s[end] != '\n' && s[end] != '"' && s[end] != ';' {
			end++
		}
		for i := end - len(separator); i >= pos+1; i-- {
			if s[i:i+len(separator)] != separator {
				continue
			}
			if i+len(separator) < len(s) && s[i+len(separator)] == '=' {
				continue
			}
			return i + len(separator), true
		}
		return 0, false
	}
}

// AutoHotkey returns the AutoHotkey language definition (a port of
// languages/autohotkey.js). The title variants need JavaScript backtracking to
// an earlier colon when the last one is followed by "=", which a post-hoc
// guard cannot express, so they are manual begins.
func AutoHotkey() *Language {
	backtick := &Mode{Begin: "\x60[\\s\\S]"}
	str := Variant(QuoteStringMode, func(m *Mode) { m.Contains = []*Mode{backtick} })

	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0

	return &Language{
		Name:            "AutoHotkey",
		Aliases:         []string{"ahk"},
		CaseInsensitive: true,
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
			{ClassName: "title", ManualBegin: ahkTitleEnd("::"), ManualFirst: ahkTitleFirsts},
			{ClassName: "title", ManualBegin: ahkTitleEnd(":"), ManualFirst: ahkTitleFirsts, HasRelevance: true, Relevance: 0},
			{ClassName: "meta", Begin: `^\s*#\w+`, End: `$`, HasRelevance: true, Relevance: 0},
			{ClassName: "built_in", Begin: `A_[a-zA-Z0-9]+`},
			{Begin: `,\s*,`},
		},
	}
}
