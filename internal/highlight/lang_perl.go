package highlight

import "strings"

func init() { RegisterLanguage("perl", Perl()) }

// perlPairedManual implements the reference's PAIRED_RE/PAIRED_DOUBLE_RE rules
// whose `\1` backreference is the chosen delimiter. prefixes are the s/tr/y or
// m/qr command names, double selects the substitution form (delimiter appears
// three times: opener, middle, closer).
func perlPairedManual(prefixes []string, double bool) func(string, int) (int, bool) {
	return func(s string, pos int) (int, bool) {
		i := pos
		matched := false
		for _, p := range prefixes {
			if strings.HasPrefix(s[i:], p) {
				i += len(p)
				matched = true
				break
			}
		}
		if !matched || i >= len(s) {
			return 0, false
		}
		d := s[i]
		if strings.IndexByte("!/|?'\"#", d) < 0 {
			return 0, false
		}
		i++
		segments := 1
		if double {
			segments = 2
		}
		for seg := 0; seg < segments; seg++ {
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if c == d {
					i++
					closed = true
					break
				}
				if c == '/' {
					return 0, false
				}
				i++
			}
			if !closed {
				return 0, false
			}
		}
		for i < len(s) && strings.IndexByte("dualxmsipngr", s[i]) >= 0 {
			i++
		}
		return i, true
	}
}

const perlModifiers = `[dualxmsipngr]{0,12}`
const perlMiddle = `(?:\\.|[^\\/])*?`

// Perl returns the Perl language definition (a port of languages/perl.js).
func Perl() *Language {
	const regexDelims = `(!|/|\||\?|'|"|#)`

	keywords := Keywords{
		"$pattern": `[\w.]+`,
		"keyword":  "abs accept alarm and atan2 bind binmode bless break caller chdir chmod chomp chop chown chr chroot close closedir connect continue cos crypt dbmclose dbmopen defined delete die do dump each else elsif endgrent endhostent endnetent endprotoent endpwent endservent eof eval exec exists exit exp fcntl fileno flock for foreach fork format formline getc getgrent getgrgid getgrnam gethostbyaddr gethostbyname gethostent getlogin getnetbyaddr getnetbyname getnetent getpeername getpgrp getpriority getprotobyname getprotobynumber getprotoent getpwent getpwnam getpwuid getservbyname getservbyport getservent getsockname getsockopt given glob gmtime goto grep gt hex if index int ioctl join keys kill last lc lcfirst length link listen local localtime log lstat lt ma map mkdir msgctl msgget msgrcv msgsnd my ne next no not oct open opendir or ord our pack package pipe pop pos print printf prototype push q|0 qq quotemeta qw qx rand read readdir readline readlink readpipe recv redo ref rename require reset return reverse rewinddir rindex rmdir say scalar seek seekdir select semctl semget semop send setgrent sethostent setnetent setpgrp setpriority setprotoent setpwent setservent setsockopt shift shmctl shmget shmread shmwrite shutdown sin sleep socket socketpair sort splice split sprintf sqrt srand stat state study sub substr symlink syscall sysopen sysread sysseek system syswrite tell telldir tie tied time times tr truncate uc ucfirst umask undef unless unlink unpack unshift untie until use utime values vec wait waitpid wantarray warn when while write x|0 xor y|0",
	}
	subst := &Mode{ClassName: "subst", Begin: `[$@]\{`, End: `\}`, Keywords: keywords}
	method := &Mode{Begin: `->\{`, End: `\}`}
	varMode := &Mode{
		Variants: []*Mode{
			{Match: `\$\d`},
			{Begin: `[$%@](\^\w\b|#\w+(::\w+)*|\{\w+\}|\w+(::\w*)*)`},
			{Begin: `[$%@][^\s\w{]`, HasRelevance: true, Relevance: 0},
		},
	}
	for i := range varMode.Variants {
		if i == 1 {
			v := varMode.Variants[i]
			v.beforeBegin = func(m *match, resp *Response) {
				next := m.index + len(m.lexeme)
				if next >= len(m.input) {
					resp.IgnoreMatch()
					return
				}
				b := m.input[next]
				if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '@' || b == '$' || b == '%' {
					resp.IgnoreMatch()
				}
			}
		}
	}
	stringContains := []*Mode{BackslashEscape, subst, varMode}

	sDoubleManual := &Mode{ClassName: "regexp", ManualBegin: perlPairedManual([]string{"s", "tr", "y"}, true), ManualFirst: "sty"}
	sRegexp := &Mode{
		ClassName: "regexp",
		Variants: []*Mode{
			sDoubleManual,
			{ClassName: "regexp", Begin: `(?:(?:s|tr|y))\(` + perlMiddle + `\)\(` + perlMiddle + `\)` + perlModifiers},
			{ClassName: "regexp", Begin: `(?:(?:s|tr|y))\[(` + perlMiddle + `)\]\[(` + perlMiddle + `)\]` + perlModifiers},
			{ClassName: "regexp", Begin: `(?:(?:s|tr|y))\{(` + perlMiddle + `)\}\{(` + perlMiddle + `)\}` + perlModifiers},
		},
		HasRelevance: true,
		Relevance:    2,
	}
	mDoubleManual := &Mode{ClassName: "regexp", ManualBegin: perlPairedManual([]string{"m", "qr"}, false), ManualFirst: "mq"}
	mRegexp := &Mode{
		ClassName: "regexp",
		Variants: []*Mode{
			Zero(Mode{ClassName: "regexp", Begin: `(m|qr)\/\/`}),
			{ClassName: "regexp", Begin: `(?:(?:(?:m|qr)?))\/` + perlMiddle + `\/` + perlModifiers},
			mDoubleManual,
			{ClassName: "regexp", Begin: `(?:(?:m|qr))\(` + perlMiddle + `\)` + perlModifiers},
			{ClassName: "regexp", Begin: `(?:(?:m|qr))\[` + perlMiddle + `\]` + perlModifiers},
			{ClassName: "regexp", Begin: `(?:(?:m|qr))\{` + perlMiddle + `\}` + perlModifiers},
		},
	}
	regexpContainer := Zero(Mode{
		Begin:    `(\/\/|` + reStartersRe + `|\b(split|return|print|reverse|grep)\b)\s*`,
		Keywords: Keywords{"keyword": "split return print reverse grep"},
		Contains: []*Mode{HashCommentMode, sRegexp, mRegexp},
	})

	perlComment := commentMode(`^=\w`, `=cut`)
	perlComment.EndsWithParent = true

	stringMode := &Mode{
		ClassName: "string",
		Contains:  stringContains,
		Variants: []*Mode{
			{ClassName: "string", Begin: `q[qwxr]?\s*\(`, End: `\)`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `q[qwxr]?\s*\[`, End: `\]`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `q[qwxr]?\s*\{`, End: `\}`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `q[qwxr]?\s*\|`, End: `\|`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `q[qwxr]?\s*<`, End: `>`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `qw\s+q`, End: `q`, HasRelevance: true, Relevance: 5},
			{ClassName: "string", Begin: `'`, End: `'`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `"`, End: `"`},
			{ClassName: "string", Begin: "`", End: "`", Contains: []*Mode{BackslashEscape}},
			Zero(Mode{ClassName: "string", Begin: `\{\w+\}`}),
			Zero(Mode{ClassName: "string", Begin: `-?\w+\s*=>`}),
		},
	}
	number := Zero(Mode{ClassName: "number", Begin: `(\b0[0-7_]+)|(\b0x[0-9a-fA-F_]+)|(\b[1-9][0-9_]*(\.[0-9_]+)?)|[0_]\b`})
	functionMode := &Mode{
		ClassName:     "function",
		BeginKeywords: "sub",
		End:           `(\s*\(.*?\))?[;{]`,
		ExcludeEnd:    true,
		HasRelevance:  true,
		Relevance:     5,
		Contains:      []*Mode{TitleMode},
	}
	dataMode := &Mode{
		Begin:       `^__DATA__$`,
		End:         `^__END__$`,
		SubLanguage: []string{"mojolicious"},
		Contains:    []*Mode{{ClassName: "comment", Begin: `^@@.*`, End: `$`}},
	}

	defaultContains := []*Mode{
		varMode,
		HashCommentMode,
		perlComment,
		method,
		stringMode,
		number,
		regexpContainer,
		functionMode,
		Zero(Mode{Begin: `-\w\b`}),
		dataMode,
	}
	subst.Contains = defaultContains
	method.Contains = defaultContains

	return &Language{
		Name:    "Perl",
		Aliases: []string{"pl", "pm"},
		Mode: Mode{
			Keywords: keywords,
			Contains: defaultContains,
		},
	}
}
