package highlight

func init() { RegisterLanguage("properties", Properties()) }

// Properties returns the .properties language definition (a port of
// languages/properties.js). The key rules are variant sets that share a
// returnBegin/contains/starts shape, so each variant is cloned from the base
// with Variant.
func Properties() *Language {
	const (
		ws0         = `[ \t\f]*`
		ws1         = `[ \t\f]+`
		equalDelim  = ws0 + `[:=]` + ws0
		delim       = `(` + equalDelim + `|` + ws1 + `)`
		keyAlphanum = `([^\\\W:= \t\f\n]|\\.)+`
		keyOther    = `([^\\:= \t\f\n]|\\.)+`
	)

	value := &Mode{
		ClassName:    "string",
		End:          `$`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{Begin: `\\\\`},
			{Begin: `\\\n`},
		},
	}
	delimAndValue := &Mode{
		End:          delim,
		HasRelevance: true,
		Relevance:    0,
		Starts:       value,
	}

	key := &Mode{
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "attr", Begin: keyAlphanum, EndsParent: true, HasRelevance: true, Relevance: 0},
		},
		Starts: delimAndValue,
	}
	key.Variants = []*Mode{
		Variant(key, func(m *Mode) { m.Begin = keyAlphanum + equalDelim; m.HasRelevance = true; m.Relevance = 1 }),
		Variant(key, func(m *Mode) { m.Begin = keyAlphanum + ws1; m.HasRelevance = true; m.Relevance = 0 }),
	}

	otherKey := &Mode{
		Begin:        keyOther + delim,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{ClassName: "meta", Begin: keyOther, EndsParent: true, HasRelevance: true, Relevance: 0},
		},
		Starts: delimAndValue,
	}

	return &Language{
		Name:            ".properties",
		CaseInsensitive: true,
		Mode: Mode{
			Illegal: `\S`,
			Contains: []*Mode{
				CommentMode(`^\s*[!#]`, `$`),
				key,
				otherKey,
				{ClassName: "attr", HasRelevance: true, Relevance: 0, Begin: keyOther + ws0 + `$`},
			},
		},
	}
}
