package highlight

import "strings"

func init() { RegisterLanguage("powershell", PowerShell()) }

const (
	powerShellTypes = "string|char|byte|int|long|bool|decimal|single|double|DateTime|xml|array|hashtable|void"

	powerShellValidVerbs = "Add|Clear|Close|Copy|Enter|Exit|Find|Format|Get|Hide|Join|Lock|" +
		"Move|New|Open|Optimize|Pop|Push|Redo|Remove|Rename|Reset|Resize|" +
		"Search|Select|Set|Show|Skip|Split|Step|Switch|Undo|Unlock|" +
		"Watch|Backup|Checkpoint|Compare|Compress|Convert|ConvertFrom|" +
		"ConvertTo|Dismount|Edit|Expand|Export|Group|Import|Initialize|" +
		"Limit|Merge|Mount|Out|Publish|Restore|Save|Sync|Unpublish|Update|" +
		"Approve|Assert|Build|Complete|Confirm|Deny|Deploy|Disable|Enable|Install|Invoke|" +
		"Register|Request|Restart|Resume|Start|Stop|Submit|Suspend|Uninstall|" +
		"Unregister|Wait|Debug|Measure|Ping|Repair|Resolve|Test|Trace|Connect|" +
		"Disconnect|Read|Receive|Send|Write|Block|Grant|Protect|Revoke|Unblock|" +
		"Unprotect|Use|ForEach|Sort|Tee|Where"

	powerShellComparisonOperators = "-and|-as|-band|-bnot|-bor|-bxor|-casesensitive|-ccontains|-ceq|-cge|-cgt|" +
		"-cle|-clike|-clt|-cmatch|-cne|-cnotcontains|-cnotlike|-cnotmatch|-contains|" +
		"-creplace|-csplit|-eq|-exact|-f|-file|-ge|-gt|-icontains|-ieq|-ige|-igt|" +
		"-ile|-ilike|-ilt|-imatch|-in|-ine|-inotcontains|-inotlike|-inotmatch|" +
		"-ireplace|-is|-isnot|-isplit|-join|-le|-like|-lt|-match|-ne|-not|" +
		"-notcontains|-notin|-notlike|-notmatch|-or|-regex|-replace|-shl|-shr|" +
		"-split|-wildcard|-xor"

	// powerShellKeywords is KEYWORDS.keyword; it doubles as PS_METHODS' keyword
	// alternation.
	powerShellKeywords = "if else foreach return do while until elseif begin for trap data dynamicparam " +
		"end break throw param continue finally in switch exit filter try process catch hidden static parameter"
)

// PowerShell returns the PowerShell language definition (a port of
// languages/powershell.js).
func PowerShell() *Language {
	titleName := &Mode{Begin: `\w[\w\d]*((-)[\w\d]+)*`}
	backtickEscape := &Mode{Begin: "\x60[\\s\\S]", HasRelevance: true, Relevance: 0}

	varBase := &Mode{ClassName: "variable"}
	varBase.Variants = []*Mode{
		Variant(varBase, func(m *Mode) { m.Begin = `\$\B` }),
		Variant(varBase, func(m *Mode) { m.ClassName = "keyword"; m.Begin = `\$this` }),
		Variant(varBase, func(m *Mode) { m.Begin = `\$[\w\d][\w\d_:]*` }),
	}
	literal := &Mode{ClassName: "literal", Begin: `\$(null|true|false)\b`}

	quoteBase := &Mode{
		ClassName: "string",
		Contains: []*Mode{
			backtickEscape,
			varBase,
			{ClassName: "variable", Begin: `\$[A-z]`, End: `[^A-z]`},
		},
	}
	quoteBase.Variants = []*Mode{
		Variant(quoteBase, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(quoteBase, func(m *Mode) { m.Begin = `@"`; m.End = `^"@` }),
	}
	aposBase := &Mode{ClassName: "string"}
	aposBase.Variants = []*Mode{
		Variant(aposBase, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(aposBase, func(m *Mode) { m.Begin = `@'`; m.End = `^'@` }),
	}

	helpTags := &Mode{ClassName: "doctag"}
	helpTags.Variants = []*Mode{
		Variant(helpTags, func(m *Mode) {
			m.Begin = `\.(synopsis|description|example|inputs|outputs|notes|link|component|role|functionality)`
		}),
		Variant(helpTags, func(m *Mode) {
			m.Begin = `\.(parameter|forwardhelptargetname|forwardhelpcategory|remotehelprunspace|externalhelp)\s+\S+`
		}),
	}

	psComment := CommentMode(``, ``)
	psComment.Contains = []*Mode{helpTags}
	psComment.Variants = []*Mode{
		Variant(psComment, func(m *Mode) { m.Begin = `#`; m.End = `$` }),
		Variant(psComment, func(m *Mode) { m.Begin = `<#`; m.End = `#>` }),
	}

	cmdlets := &Mode{ClassName: "built_in"}
	cmdlets.Variants = []*Mode{
		Variant(cmdlets, func(m *Mode) { m.Begin = `(` + powerShellValidVerbs + `)+(-)[\w\d]+` }),
	}

	psClass := &Mode{
		ClassName:     "class",
		BeginKeywords: "class enum",
		End:           `\s*[{]`,
		ExcludeEnd:    true,
		HasRelevance:  true,
		Relevance:     0,
		Contains:      []*Mode{TitleMode},
	}

	psFunction := &Mode{
		ClassName:    "function",
		Begin:        `function\s+`,
		End:          `\s*\{|$`,
		ExcludeEnd:   true,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{Begin: "function", HasRelevance: true, Relevance: 0, ClassName: "keyword"},
			{ClassName: "title", Begin: titleName.Begin, HasRelevance: true, Relevance: 0},
			{ClassName: "params", Begin: `\(`, End: `\)`, HasRelevance: true, Relevance: 0, Contains: []*Mode{varBase}},
		},
	}

	psUsing := &Mode{
		Begin:       `using\s`,
		End:         `$`,
		ReturnBegin: true,
		Contains: []*Mode{
			quoteBase,
			aposBase,
			{ClassName: "keyword", Begin: `(using|assembly|command|module|namespace|type)`},
		},
	}

	psArguments := &Mode{Variants: []*Mode{
		{ClassName: "operator", Begin: `(` + powerShellComparisonOperators + `)\b`},
		{ClassName: "literal", Begin: `(-)[\w\d]+`, HasRelevance: true, Relevance: 0},
	}}

	hashSigns := &Mode{ClassName: "selector-tag", Begin: `@\B`, HasRelevance: true, Relevance: 0}

	keywordAlternation := strings.ReplaceAll(powerShellKeywords, " ", "|")
	methods := &Mode{
		ClassName:    "function",
		Begin:        `\[.*\]\s*[\w]+[ ]??\(`,
		End:          `$`,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{ClassName: "keyword", Begin: `(` + keywordAlternation + `)\b`, EndsParent: true, HasRelevance: true, Relevance: 0},
			Variant(TitleMode, func(m *Mode) { m.EndsParent = true }),
		},
	}

	gentlemansSet := []*Mode{
		methods,
		psComment,
		backtickEscape,
		NumberMode,
		quoteBase,
		aposBase,
		cmdlets,
		varBase,
		literal,
		hashSigns,
	}

	psType := &Mode{
		Begin:        `\[`,
		End:          `\]`,
		ExcludeBegin: true,
		ExcludeEnd:   true,
		HasRelevance: true,
		Relevance:    0,
	}
	typeContains := []*Mode{Self}
	typeContains = append(typeContains, gentlemansSet...)
	typeContains = append(typeContains,
		&Mode{Begin: `(` + powerShellTypes + `)`, ClassName: "built_in", HasRelevance: true, Relevance: 0},
		&Mode{ClassName: "type", Begin: `[\.\w\d]+`, HasRelevance: true, Relevance: 0},
	)
	psType.Contains = typeContains

	methods.Contains = append([]*Mode{psType}, methods.Contains...)

	rootContains := append([]*Mode{}, gentlemansSet...)
	rootContains = append(rootContains, psClass, psFunction, psUsing, psArguments, psType)

	return &Language{
		Name:            "PowerShell",
		Aliases:         []string{"ps", "ps1"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "-?[A-z\\.\\-]+\\b",
				"keyword": "if else foreach return do while until elseif begin for trap data dynamicparam end break throw param " +
					"continue finally in switch exit filter try process catch hidden static parameter",
				"built_in": "ac asnp cat cd CFS chdir clc clear clhy cli clp cls clv cnsn compare copy cp cpi cpp curl cvpa dbp " +
					"del diff dir dnsn ebp echo|0 epal epcsv epsn erase etsn exsn fc fhx fl ft fw gal gbp gc gcb gci gcm " +
					"gcs gdr gerr ghy gi gin gjb gl gm gmo gp gps gpv group gsn gsnp gsv gtz gu gv gwmi h history icm iex " +
					"ihy ii ipal ipcsv ipmo ipsn irm ise iwmi iwr kill lp ls man md measure mi mount move mp mv nal ndr " +
					"ni nmo npssc nsn nv ogv oh popd ps pushd pwd r rbp rcjb rcsn rd rdr ren ri rjb rm rmdir rmo rni rnp " +
					"rp rsn rsnp rujb rv rvpa rwmi sajb sal saps sasv sbp sc scb select set shcm si sl sleep sls sort sp " +
					"spjb spps spsv start stz sujb sv swmi tee trcm type wget where wjb write",
			},
			Contains: rootContains,
		},
	}
}
