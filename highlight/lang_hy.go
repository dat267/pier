package highlight

func init() { RegisterLanguage("hy", Hy()) }

// Hy returns the Hy language definition (a port of languages/hy.js).
func Hy() *Language {
	const symbolStart = `a-zA-Z_\-!.?+*=<>&#'`
	symbolRe := `[` + symbolStart + `][` + symbolStart + `0-9/;:]*`

	nameKeywords := Keywords{
		"$pattern": "[a-zA-Z_\\-!.?+*=<>&#'][a-zA-Z_\\-!.?+*=<>&#'0-9/;:]*",
		"builtin-name": "!= % %= & &= * ** **= *= *map + += , --build-class-- --import-- -= . / // //= /= < << <<= <= = > >= " +
			">> >>= @ @= ^ ^= abs accumulate all and any ap-compose ap-dotimes ap-each ap-each-while ap-filter " +
			"ap-first ap-if ap-last ap-map ap-map-when ap-pipe ap-reduce ap-reject apply as-> ascii assert assoc " +
			"bin break butlast callable calling-module-name car case cdr chain chr coll? combinations compile " +
			"compress cond cons cons? continue count curry cut cycle dec def default-method defclass defmacro " +
			"defmacro-alias defmacro/g! defmain defmethod defmulti defn defn-alias defnc defnr defreader defseq " +
			"del delattr delete-route dict-comp dir disassemble dispatch-reader-macro distinct divmod do doto " +
			"drop drop-last drop-while empty? end-sequence eval eval-and-compile eval-when-compile even? every? " +
			"except exec filter first flatten float? fn fnc fnr for for* format fraction genexpr gensym get " +
			"getattr global globals group-by hasattr hash hex id identity if if* if-not if-python2 import in inc " +
			"input instance? integer integer-char? integer? interleave interpose is is-coll is-cons is-empty " +
			"is-even is-every is-float is-instance is-integer is-integer-char is-iterable is-iterator is-keyword " +
			"is-neg is-none is-not is-numeric is-odd is-pos is-string is-symbol is-zero isinstance islice " +
			"issubclass iter iterable? iterate iterator? keyword keyword? lambda last len let lif lif-not list* " +
			"list-comp locals loop macro-error macroexpand macroexpand-1 macroexpand-all map max merge-with " +
			"method-decorator min multi-decorator multicombinations name neg? next none? nonlocal not not-in not? " +
			"nth numeric? oct odd? open or ord partition permutations pos? post-route postwalk pow prewalk print " +
			"product profile/calls profile/cpu put-route quasiquote quote raise range read read-str " +
			"recursive-replace reduce remove repeat repeatedly repr require rest round route route-with-methods " +
			"rwm second seq set-comp setattr setv some sorted string string? sum switch symbol? take take-nth " +
			"take-while tee try unless unquote unquote-splicing vars walk when while with with* with-decorator " +
			"with-gensyms xi xor yield yield-from zero? zip zip-longest | |= ~",
	}

	symbol := &Mode{Begin: symbolRe, HasRelevance: true, Relevance: 0}
	number := &Mode{ClassName: "number", Begin: `[-+]?\d+(\.\d+)?`, HasRelevance: true, Relevance: 0}
	str := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0
	literal := &Mode{ClassName: "literal", Begin: `\b([Tt]rue|[Ff]alse|nil|None)\b`}
	collection := &Mode{Begin: `[\[\{]`, End: `[\]\}]`}
	hint := &Mode{ClassName: "comment", Begin: `\^` + symbolRe}
	hintCol := CommentMode(`\^\{`, `\}`)
	key := &Mode{ClassName: "symbol", Begin: `[:]{1,2}` + symbolRe}
	list := &Mode{Begin: `\(`, End: `\)`}
	body := &Mode{EndsWithParent: true, HasRelevance: true, Relevance: 0}
	name := &Mode{
		ClassName:    "name",
		Begin:        symbolRe,
		Keywords:     nameKeywords,
		HasRelevance: true,
		Relevance:    0,
		Starts:       body,
	}

	defaultContains := []*Mode{list, str, hint, hintCol, comment, key, collection, number, literal, symbol}
	list.Contains = []*Mode{CommentMode(`comment`, `\B|\b`), name, body}
	body.Contains = defaultContains
	collection.Contains = defaultContains

	return &Language{
		Name:    "Hy",
		Aliases: []string{"hylang"},
		Mode: Mode{
			Illegal: `\S`,
			Contains: []*Mode{
				Shebang("", 0),
				list,
				str,
				hint,
				hintCol,
				comment,
				key,
				collection,
				number,
				literal,
			},
		},
	}
}
