package highlight

func init() { RegisterLanguage("tcl", Tcl()) }

// Tcl returns the Tcl language definition (a port of languages/tcl.js).
func Tcl() *Language {
	number := &Mode{ClassName: "number"}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = BinaryNumberRe }),
		Variant(number, func(m *Mode) { m.Begin = CNumberRe }),
	}

	title := &Mode{
		ClassName:      "title",
		Begin:          `[ \t\n\r]+(::)?[a-zA-Z_]((::)?[a-zA-Z0-9_])*`,
		End:            `[ \t\n\r]`,
		EndsWithParent: true,
		ExcludeEnd:     true,
	}
	proc := &Mode{
		BeginKeywords: "proc",
		End:           `[\{]`,
		ExcludeEnd:    true,
		Contains:      []*Mode{title},
	}

	variable := &Mode{ClassName: "variable"}
	variable.Variants = []*Mode{
		Variant(variable, func(m *Mode) {
			m.Begin = `\$(::)?[a-zA-Z_][a-zA-Z0-9_]*(::[a-zA-Z_][a-zA-Z0-9_]*)*`
		}),
		Variant(variable, func(m *Mode) {
			m.Begin = `\$\{(::)?[a-zA-Z_]((::)?[a-zA-Z0-9_])*`
			m.End = `\}`
			m.Contains = []*Mode{number}
		}),
	}

	// The single quoted-string variant inherits the string class and the
	// backslash-escape contains and drops the illegal newline.
	strMode := &Mode{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{BackslashEscape}}

	return &Language{
		Name:    "Tcl",
		Aliases: []string{"tk"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "after append apply array auto_execok auto_import auto_load auto_mkindex " +
					"auto_mkindex_old auto_qualify auto_reset bgerror binary break catch cd chan clock " +
					"close concat continue dde dict encoding eof error eval exec exit expr fblocked " +
					"fconfigure fcopy file fileevent filename flush for foreach format gets glob global " +
					"history http if incr info interp join lappend|10 lassign|10 lindex|10 linsert|10 list " +
					"llength|10 load lrange|10 lrepeat|10 lreplace|10 lreverse|10 lsearch|10 lset|10 lsort|10 " +
					"mathfunc mathop memory msgcat namespace open package parray pid pkg::create pkg_mkIndex " +
					"platform platform::shell proc puts pwd read refchan regexp registry regsub|10 rename " +
					"return safe scan seek set socket source split string subst switch tcl_endOfWord " +
					"tcl_findLibrary tcl_startOfNextWord tcl_startOfPreviousWord tcl_wordBreakAfter " +
					"tcl_wordBreakBefore tcltest tclvars tell time tm trace unknown unload unset update " +
					"uplevel upvar variable vwait while",
			},
			Contains: []*Mode{
				CommentMode(`;[ \t]*#`, `$`),
				CommentMode(`^[ \t]*#`, `$`),
				proc,
				variable,
				strMode,
				number,
			},
		},
	}
}
