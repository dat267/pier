package highlight

func init() { RegisterLanguage("applescript", AppleScript()) }

// AppleScript returns the AppleScript language definition (a port of
// languages/applescript.js).
func AppleScript() *Language {
	str := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })

	params := &Mode{
		ClassName: "params",
		Begin:     `\(`,
		End:       `\)`,
		Contains:  []*Mode{Self, CNumberMode, str},
	}

	comment1 := CommentMode(`--`, `$`)
	comment2 := CommentMode(`\(\*`, `\*\)`)
	comment2.Contains = append([]*Mode{Self, comment1}, comment2.Contains...)

	keywordPatterns := []string{
		`apart from`,
		`aside from`,
		`instead of`,
		`out of`,
		`greater than`,
		`isn't|(doesn't|does not) (equal|come before|come after|contain)`,
		`(greater|less) than( or equal)?`,
		`(starts?|ends|begins?) with`,
		`contained by`,
		`comes (before|after)`,
		`a (ref|reference)`,
		`POSIX (file|path)`,
		`(date|time) string`,
		`quoted form`,
	}
	builtInPatterns := []string{
		`clipboard info`,
		`the clipboard`,
		`info for`,
		`list (disks|folder)`,
		`mount volume`,
		`path to`,
		`(close|open for) access`,
		`(get|set) eof`,
		`current date`,
		`do shell script`,
		`get volume settings`,
		`random number`,
		`set volume`,
		`system attribute`,
		`system info`,
		`time to GMT`,
		`(load|run|store) script`,
		`scripting components`,
		`ASCII (character|number)`,
		`localized string`,
		`choose (application|color|file|file name|folder|from list|remote application|URL)`,
		`display (alert|dialog)`,
	}

	return &Language{
		Name:    "AppleScript",
		Aliases: []string{"osascript"},
		Keywords: Keywords{
			"keyword": "about above after against and around as at back before beginning " +
				"behind below beneath beside between but by considering " +
				"contain contains continue copy div does eighth else end equal " +
				"equals error every exit fifth first for fourth from front " +
				"get given global if ignoring in into is it its last local me " +
				"middle mod my ninth not of on onto or over prop property put ref " +
				"reference repeat returning script second set seventh since " +
				"sixth some tell tenth that the|0 then third through thru " +
				"timeout times to transaction try until where while whose with " +
				"without",
			"literal": "AppleScript false linefeed return pi quote result space tab true",
			"built_in": "alias application boolean class constant date file integer list " +
				"number real record string text " +
				"activate beep count delay launch log offset read round " +
				"run say summarize write " +
				"character characters contents day frontmost id item length " +
				"month name paragraph paragraphs rest reverse running time version " +
				"weekday word words year",
		},
		Illegal: `\/\/|->|=>|\[\[`,
		Contains: []*Mode{
			str,
			CNumberMode,
			{ClassName: "built_in", Begin: `\b` + eitherJoin(builtInPatterns) + `\b`},
			{ClassName: "built_in", Begin: `^\s*return\b`},
			{ClassName: "literal", Begin: `\b(text item delimiters|current application|missing value)\b`},
			{ClassName: "keyword", Begin: `\b` + eitherJoin(keywordPatterns) + `\b`},
			{
				BeginKeywords: "on",
				Illegal:       `[${=;\n]`,
				Contains:      []*Mode{UnderscoreTitleMode, params},
			},
			comment1,
			comment2,
			HashCommentMode,
		},
	}
}
