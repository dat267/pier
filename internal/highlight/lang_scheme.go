package highlight

func init() { RegisterLanguage("scheme", Scheme()) }

// Scheme returns the Scheme language definition (a port of languages/scheme.js).
func Scheme() *Language {
	const identRe = "[^\\(\\)\\[\\]\\{\\}\",'`;#|\\\\\\s]+"
	const simpleNumberRe = `(-|\+)?\d+([./]\d+)?`
	complexNumberRe := simpleNumberRe + `[+\-]` + simpleNumberRe + `i`
	const literalBegin = "(#t|#f|#\\\\[^\\(\\)\\[\\]\\{\\}\",'`;#|\\\\\\s]+|#\\\\.)"

	keywords := Keywords{
		"$pattern": "[^\\(\\)\\[\\]\\{\\}\",'`;#|\\\\\\s]+",
		"builtin-name": "case-lambda call/cc class define-class exit-handler field import inherit init-field " +
			"interface let*-values let-values let/ec mixin opt-lambda override protect provide public " +
			"rename require require-for-syntax syntax syntax-case syntax-error unit/sig unless when " +
			"with-syntax and begin call-with-current-continuation call-with-input-file " +
			"call-with-output-file case cond define define-syntax delay do dynamic-wind else for-each " +
			"if lambda let let* let-syntax letrec letrec-syntax map or syntax-rules ' * + , ,@ - ... / " +
			"; < <= = => > >= ` abs acos angle append apply asin assoc assq assv atan boolean? caar " +
			"cadr call-with-input-file call-with-output-file call-with-values car cdddar cddddr cdr " +
			"ceiling char->integer char-alphabetic? char-ci<=? char-ci<? char-ci=? char-ci>=? char-ci>? " +
			"char-downcase char-lower-case? char-numeric? char-ready? char-upcase char-upper-case? " +
			"char-whitespace? char<=? char<? char=? char>=? char>? char? close-input-port " +
			"close-output-port complex? cons cos current-input-port current-output-port denominator " +
			"display eof-object? eq? equal? eqv? eval even? exact->inexact exact? exp expt floor force " +
			"gcd imag-part inexact->exact inexact? input-port? integer->char integer? " +
			"interaction-environment lcm length list list->string list->vector list-ref list-tail list? " +
			"load log magnitude make-polar make-rectangular make-string make-vector max member memq " +
			"memv min modulo negative? newline not null-environment null? number->string number? " +
			"numerator odd? open-input-file open-output-file output-port? pair? peek-char port? " +
			"positive? procedure? quasiquote quote quotient rational? rationalize read read-char " +
			"real-part real? remainder reverse round scheme-report-environment set! set-car! set-cdr! " +
			"sin sqrt string string->list string->number string->symbol string-append string-ci<=? " +
			"string-ci<? string-ci=? string-ci>=? string-ci>? string-copy string-fill! string-length " +
			"string-ref string-set! string<=? string<? string=? string>=? string>? string? substring " +
			"symbol->string symbol? tan transcript-off transcript-on truncate values vector " +
			"vector->list vector-fill! vector-length vector-ref vector-set! with-input-from-file " +
			"with-output-to-file write write-char zero?",
	}

	literal := &Mode{ClassName: "literal", Begin: literalBegin}

	numberBase := &Mode{ClassName: "number"}
	numberBase.Variants = []*Mode{
		Variant(numberBase, func(m *Mode) { m.Begin = simpleNumberRe; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(numberBase, func(m *Mode) { m.Begin = complexNumberRe; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(numberBase, func(m *Mode) { m.Begin = `#b[0-1]+(/[0-1]+)?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `#o[0-7]+(/[0-7]+)?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `#x[0-9a-f]+(/[0-9a-f]+)?` }),
	}

	c1 := CommentMode(`;`, `$`)
	c1.HasRelevance = true
	c1.Relevance = 0
	c2 := CommentMode(`#\|`, `\|#`)
	comments := []*Mode{c1, c2}

	ident := &Mode{Begin: identRe, HasRelevance: true, Relevance: 0}
	quotedIdent := &Mode{ClassName: "symbol", Begin: `'` + identRe}
	body := &Mode{EndsWithParent: true, HasRelevance: true, Relevance: 0}

	quotedList := &Mode{Contains: []*Mode{
		{Begin: `\(`, End: `\)`, Contains: []*Mode{Self, literal, QuoteStringMode, numberBase, ident, quotedIdent}},
	}}
	quotedList.Variants = []*Mode{
		Variant(quotedList, func(m *Mode) { m.Begin = `'` }),
		Variant(quotedList, func(m *Mode) { m.Begin = "\x60" }),
	}

	name := &Mode{ClassName: "name", HasRelevance: true, Relevance: 0, Begin: identRe, Keywords: keywords}

	lambda := &Mode{
		Begin:          `lambda`,
		EndsWithParent: true,
		ReturnBegin:    true,
		Contains: []*Mode{
			name,
			{
				EndsParent: true,
				Variants: []*Mode{
					{Begin: `\(`, End: `\)`},
					{Begin: `\[`, End: `\]`},
				},
				Contains: []*Mode{ident},
			},
		},
	}

	list := &Mode{Contains: []*Mode{lambda, name, body}}
	list.Variants = []*Mode{
		Variant(list, func(m *Mode) { m.Begin = `\(`; m.End = `\)` }),
		Variant(list, func(m *Mode) { m.Begin = `\[`; m.End = `\]` }),
	}

	body.Contains = []*Mode{literal, numberBase, QuoteStringMode, ident, quotedIdent, quotedList, list, c1, c2}

	contains := []*Mode{Shebang("", 0), numberBase, QuoteStringMode, quotedIdent, quotedList, list}
	contains = append(contains, comments...)

	return &Language{
		Name:     "Scheme",
		Illegal:  `\S`,
		Contains: contains,
	}
}
