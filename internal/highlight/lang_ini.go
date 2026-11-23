package highlight

func init() { RegisterLanguage("ini", INI()) }

// iniKeyGuard ports the reference's `(?=\s*=\s*[^#\s])` lookahead on the dotted
// key. Hand-written for the same reason as scanIniKey: an `\A`-anchored regexp
// that cannot be prefixed costs a full pass over the remaining input per call.
func iniKeyGuard(s string) bool {
	i := skipJSSpace(s, 0)
	if i >= len(s) || s[i] != '=' {
		return false
	}
	i = skipJSSpace(s, i+1)
	if i >= len(s) {
		return false
	}
	if jsSpaceWidth(s, i) > 0 || s[i] == '#' {
		return false
	}
	return true
}

// INI returns the TOML/INI language definition (a port of languages/ini.js).
func INI() *Language {
	comments := &Mode{
		ClassName: "comment",
		Contains: []*Mode{
			PhrasalWordsMode,
			Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}),
		},
	}
	comments.Variants = []*Mode{
		Variant(comments, func(m *Mode) { m.Begin = `;`; m.End = `$` }),
		Variant(comments, func(m *Mode) { m.Begin = `#`; m.End = `$` }),
	}

	numbers := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numbers.Variants = []*Mode{
		Variant(numbers, func(m *Mode) { m.Begin = `([+-]+)?[\d]+_[\d_]+` }),
		Variant(numbers, func(m *Mode) { m.Begin = NumberRe }),
	}

	variables := &Mode{ClassName: "variable"}
	variables.Variants = []*Mode{
		Variant(variables, func(m *Mode) { m.Begin = `\$[\w\d"][\w\d_]*` }),
		Variant(variables, func(m *Mode) { m.Begin = `\$\{(.*?)\}` }),
	}

	literals := &Mode{ClassName: "literal", Begin: `\bon|off|true|false|yes|no\b`}

	strings := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape}}
	strings.Variants = []*Mode{
		Variant(strings, func(m *Mode) { m.Begin = `'''`; m.End = `'''`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(strings, func(m *Mode) { m.Begin = `"""`; m.End = `"""`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(strings, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(strings, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
	}

	array := &Mode{
		Begin:        `\[`,
		End:          `\]`,
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{comments, literals, variables, strings, numbers, Self},
	}

	// A rejected lookahead must leave the character for the root illegal rule.
	// The scan is hand-written: running `\A` plus this alternation through Go's
	// regexp costs a full pass over the remaining input per call (2.5 ms on a
	// 24 KiB subject) because the pattern cannot be prefixed, and the port calls
	// it at every key byte.
	dottedKey := &Mode{
		ClassName:   "attr",
		ManualFirst: `-_"'0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ`,
		ManualBegin: func(s string, pos int) (int, bool) {
			end, ok := scanIniKey(s, pos)
			if !ok {
				return 0, false
			}
			return end, iniKeyGuard(s[end:])
		},
		Starts: &Mode{
			End:      `$`,
			Contains: []*Mode{comments, array, literals, variables, strings, numbers},
		},
	}

	return &Language{
		Name:            "TOML, also INI",
		Aliases:         []string{"toml"},
		CaseInsensitive: true,
		Illegal:         `\S`,
		Contains: []*Mode{
			comments,
			{ClassName: "section", Begin: `\[+`, End: `\]+`},
			dottedKey,
		},
	}
}

// scanIniKey ports the reference's dotted-key lookahead, `([A-Za-z0-9_-]+|
// "(\\"|[^"])*"|'[^']*')(\s*\.\s*...[A-Za-z0-9_-]...)*`, from pos. It returns the
// end of the key as a byte offset into s. An unterminated quoted key fails, as
// the regexp's alternation does when no branch completes.
func scanIniKey(s string, pos int) (int, bool) {
	end, ok := scanIniKeySegment(s, pos)
	if !ok {
		return 0, false
	}
	for {
		// `\s*\.\s*` then another segment; a failed iteration stops the loop
		// without consuming, which is what the regexp's star does.
		i := skipJSSpace(s, end)
		if i >= len(s) || s[i] != '.' {
			return end, true
		}
		afterDot := skipJSSpace(s, i+1)
		next, ok := scanIniKeySegment(s, afterDot)
		if !ok {
			// The regexp backtracks to the shorter match that stops before the
			// dot, so the star takes zero more iterations.
			return end, true
		}
		end = next
	}
}

// scanIniKeySegment scans one `[A-Za-z0-9_-]+|"..."|'...'` alternative.
func scanIniKeySegment(s string, pos int) (int, bool) {
	if pos >= len(s) {
		return 0, false
	}
	switch c := s[pos]; {
	case c == '"':
		for i := pos + 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++ // `\\"` consumes the escaped byte
			case '"':
				return i + 1, true
			}
		}
		return 0, false
	case c == '\'':
		for i := pos + 1; i < len(s); i++ {
			if s[i] == '\'' {
				return i + 1, true
			}
		}
		return 0, false
	}
	end := pos
	for end < len(s) && (isIniKeyByte(s[end])) {
		end++
	}
	return end, end > pos
}

func isIniKeyByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}
