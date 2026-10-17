package highlight

func init() { RegisterLanguage("lisp", Lisp()) }

// Lisp returns the Lisp language definition (a port of languages/lisp.js).
// MEC_RE is the reference's `\|[^]*?\|`; Go's RE2 has no `[^]`, so `[\s\S]`
// stands in (both match any character including a newline).
func Lisp() *Language {
	const lispIdentRe = `[a-zA-Z_\-+\*\/<=>&#][a-zA-Z0-9_\-+*\/<=>&#!]*`
	const mecRe = `\|[\s\S]*?\|`
	const simpleNumberRe = `(-|\+)?\d+(\.\d+|\/\d+)?((d|e|f|l|s|D|E|F|L|S)(\+|-)?\d+)?`

	literal := &Mode{ClassName: "literal", Begin: `\b(t{1}|nil)\b`}

	number := &Mode{ClassName: "number"}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = simpleNumberRe; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(number, func(m *Mode) { m.Begin = `#(b|B)[0-1]+(/[0-1]+)?` }),
		Variant(number, func(m *Mode) { m.Begin = `#(o|O)[0-7]+(/[0-7]+)?` }),
		Variant(number, func(m *Mode) { m.Begin = `#(x|X)[0-9a-fA-F]+(/[0-9a-fA-F]+)?` }),
		Variant(number, func(m *Mode) {
			m.Begin = `#(c|C)\(` + simpleNumberRe + ` +` + simpleNumberRe
			m.End = `\)`
		}),
	}

	str := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0

	variable := &Mode{Begin: `\*`, End: `\*`}
	keyword := &Mode{ClassName: "symbol", Begin: `[:&]` + lispIdentRe}
	ident := &Mode{Begin: lispIdentRe, HasRelevance: true, Relevance: 0}
	mec := &Mode{Begin: mecRe}

	quotedList := &Mode{Begin: `\(`, End: `\)`, Contains: []*Mode{Self, literal, str, number, ident}}

	quoted := &Mode{Contains: []*Mode{number, str, variable, keyword, quotedList, ident}}
	quoted.Variants = []*Mode{
		Variant(quoted, func(m *Mode) { m.Begin = `['` + "`" + `]\(`; m.End = `\)` }),
		Variant(quoted, func(m *Mode) { m.Begin = `\(quote `; m.End = `\)`; m.Keywords = Keywords{"name": "quote"} }),
		Variant(quoted, func(m *Mode) { m.Begin = `'` + mecRe }),
	}

	quotedAtom := &Mode{}
	quotedAtom.Variants = []*Mode{
		Variant(quotedAtom, func(m *Mode) { m.Begin = `'` + lispIdentRe }),
		Variant(quotedAtom, func(m *Mode) { m.Begin = `#'` + lispIdentRe + `(::` + lispIdentRe + `)*` }),
	}

	list := &Mode{Begin: `\(\s*`, End: `\)`}
	body := &Mode{EndsWithParent: true, HasRelevance: true, Relevance: 0}
	nameBase := &Mode{ClassName: "name"}
	nameBase.Variants = []*Mode{
		Variant(nameBase, func(m *Mode) { m.Begin = lispIdentRe; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(nameBase, func(m *Mode) { m.Begin = mecRe }),
	}
	list.Contains = []*Mode{nameBase, body}
	body.Contains = []*Mode{quoted, quotedAtom, list, literal, number, str, comment, variable, keyword, mec, ident}

	return &Language{
		Name: "Lisp",
		Mode: Mode{
			Illegal: `\S`,
			Contains: []*Mode{
				number,
				Shebang("", 0),
				literal,
				str,
				comment,
				quoted,
				quotedAtom,
				list,
				ident,
			},
		},
	}
}
