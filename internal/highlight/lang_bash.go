package highlight

import "strings"

func init() { RegisterLanguage("bash", Bash()) }

// Bash returns the Bash language definition (a port of languages/bash.js).
func Bash() *Language {
	varMode := &Mode{}
	bracedVar := &Mode{
		ClassName: "variable",
		Begin:     `\$\{`,
		End:       `\}`,
		Contains: []*Mode{
			Self,
			{Begin: `:-`, Contains: []*Mode{varMode}},
		},
	}
	// The reference's `$[\w\d#@][\w\d_]*(?![\w\d])(?![$])` negative
	// lookaheads become a post-match guard.
	varWord := &Mode{
		ClassName: "variable",
		Begin:     `\$[\w\d#@][\w\d_]*`,
		beforeBegin: func(m *match, resp *Response) {
			next := m.index + len(m.lexeme)
			if next < len(m.input) {
				b := m.input[next]
				if isWordByte(b) || b == '$' {
					resp.IgnoreMatch()
				}
			}
		},
	}
	varMode.ClassName = "variable"
	varMode.Variants = []*Mode{varWord, bracedVar}

	subst := &Mode{
		ClassName: "subst",
		Begin:     `\$\(`,
		End:       `\)`,
		Contains:  []*Mode{BackslashEscape},
	}
	quoteString := &Mode{
		ClassName: "string",
		Begin:     `"`,
		End:       `"`,
		Contains:  []*Mode{BackslashEscape, varMode, subst},
	}
	subst.Contains = append(subst.Contains, quoteString)

	// The reference's `<<-?\s*(?=\w+)` positive lookahead becomes a guard.
	hereDoc := &Mode{
		Begin: `<<-?\s*`,
		beforeBegin: func(m *match, resp *Response) {
			next := m.index + len(m.lexeme)
			if next >= len(m.input) || !isWordByte(m.input[next]) {
				resp.IgnoreMatch()
			}
		},
		Starts: &Mode{
			Contains: []*Mode{
				EndSameAsBegin(&Mode{ClassName: "string", Begin: `(\w+)`, End: `(\w+)`}),
			},
		},
	}
	escapedQuote := &Mode{ClassName: "", Begin: `\\"`}
	aposString := &Mode{ClassName: "string", Begin: `'`, End: `'`}
	arithmetic := &Mode{
		Begin: `\$\(\(`,
		End:   `\)\)`,
		Contains: []*Mode{
			{ClassName: "number", Begin: `\d+#[0-9a-f]+`},
			NumberMode,
			varMode,
		},
	}
	shells := []string{"fish", "bash", "zsh", "sh", "csh", "ksh", "tcsh", "dash", "scsh"}
	binary := `(` + joinStrings(shells, "|") + `)`
	function := Zero(Mode{
		ClassName:   "function",
		Begin:       `\w[\w\d_]*\s*\(\s*\)\s*\{`,
		ReturnBegin: true,
		Contains:    []*Mode{Variant(TitleMode, func(m *Mode) { m.Begin = `\w[\w\d_]*` })},
	})

	keywords := Keywords{
		"$pattern": `\b[a-z._-]+\b`,
		"keyword":  "if then else elif fi for while in do done case esac function",
		"literal":  "true false",
		"built_in": "break cd continue eval exec exit export getopts hash pwd readonly return shift test times " +
			"trap umask unset alias bind builtin caller command declare echo enable help let local logout mapfile printf " +
			"read readarray source type typeset ulimit unalias set shopt " +
			"autoload bg bindkey bye cap chdir clone comparguments compcall compctl compdescribe compfiles " +
			"compgroups compquote comptags comptry compvalues dirs disable disown echotc echoti emulate " +
			"fc fg float functions getcap getln history integer jobs kill limit log noglob popd print " +
			"pushd pushln rehash sched setcap setopt stat suspend ttyctl unfunction unhash unlimit " +
			"unsetopt vared wait whence where which zcompile zformat zftp zle zmodload zparseopts zprof " +
			"zpty zregexparse zsocket zstyle ztcp",
	}

	return &Language{
		Name:     "Bash",
		Aliases:  []string{"sh", "zsh"},
		Keywords: keywords,
		Contains: []*Mode{
			Shebang(binary, 10),
			Shebang("", 0),
			function,
			arithmetic,
			HashCommentMode,
			hereDoc,
			quoteString,
			escapedQuote,
			aposString,
			varMode,
		},
	}
}

func joinStrings(values []string, sep string) string {
	var out strings.Builder
	for i, v := range values {
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(v)
	}
	return out.String()
}
