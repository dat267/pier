package highlight

func init() { RegisterLanguage("clojure", Clojure()) }

// Clojure returns the Clojure language definition (a port of languages/clojure.js).
// Its collection rules are recursive, so the modes are linked with pointers
// after construction.
func Clojure() *Language {
	const symbolStart = `a-zA-Z_\-!.?+*=<>&#'`
	symbolRe := `[` + symbolStart + `][` + symbolStart + `0-9/;:]*`
	const globalWords = "def defonce defprotocol defstruct defmulti defmethod defn- defn defmacro deftype defrecord"

	nameKeywords := Keywords{
		"$pattern": "[a-zA-Z_\\-!.?+*=<>&#'][a-zA-Z_\\-!.?+*=<>&#'0-9/;:]*",
		"builtin-name": "def defonce defprotocol defstruct defmulti defmethod defn- defn defmacro deftype defrecord cond " +
			"apply if-not if-let if not not= =|0 <|0 >|0 <=|0 >=|0 ==|0 +|0 /|0 *|0 -|0 rem quot neg? pos? delay? " +
			"symbol? keyword? true? false? integer? empty? coll? list? set? ifn? fn? associative? sequential? " +
			"sorted? counted? reversible? number? decimal? class? distinct? isa? float? rational? reduced? ratio? " +
			"odd? even? char? seq? vector? string? map? nil? contains? zero? instance? not-every? not-any? " +
			"libspec? -> ->> .. . inc compare do dotimes mapcat take remove take-while drop letfn drop-last " +
			"take-last drop-while while intern condp case reduced cycle split-at split-with repeat replicate " +
			"iterate range merge zipmap declare line-seq sort comparator sort-by dorun doall nthnext nthrest " +
			"partition eval doseq await await-for let agent atom send send-off release-pending-sends add-watch " +
			"mapv filterv remove-watch agent-error restart-agent set-error-handler error-handler set-error-mode! " +
			"error-mode shutdown-agents quote var fn loop recur throw try monitor-enter monitor-exit macroexpand " +
			"macroexpand-1 for dosync and or when when-not when-let comp juxt partial sequence memoize constantly " +
			"complement identity assert peek pop doto proxy first rest cons cast coll last butlast sigs reify " +
			"second ffirst fnext nfirst nnext meta with-meta ns in-ns create-ns import refer keys select-keys " +
			"vals key val rseq name namespace promise into transient persistent! conj! assoc! dissoc! pop! disj! " +
			"use class type num float double short byte boolean bigint biginteger bigdec print-method print-dup " +
			"throw-if printf format load compile get-in update-in pr pr-on newline flush read slurp read-line " +
			"subvec with-open memfn time re-find re-groups rand-int rand mod locking assert-valid-fdecl alias " +
			"resolve ref deref refset swap! reset! set-validator! compare-and-set! alter-meta! reset-meta! " +
			"commute get-validator alter ref-set ref-history-count ref-min-history ref-max-history ensure sync " +
			"io! new next conj set! to-array future future-call into-array aset gen-class reduce map filter find " +
			"empty hash-map hash-set sorted-map sorted-map-by sorted-set sorted-set-by vec vector seq flatten " +
			"reverse assoc dissoc list disj get union difference intersection extend extend-type extend-protocol " +
			"int nth delay count concat chunk chunk-buffer chunk-append chunk-first chunk-rest max min dec " +
			"unchecked-inc-int unchecked-inc unchecked-dec-inc unchecked-dec unchecked-negate unchecked-add-int " +
			"unchecked-add unchecked-subtract-int unchecked-subtract chunk-next chunk-cons chunked-seq? prn " +
			"vary-meta lazy-seq spread list* str find-keyword keyword symbol gensym force rationalize",
	}

	symbol := &Mode{Begin: symbolRe, HasRelevance: true, Relevance: 0}
	number := &Mode{ClassName: "number", Begin: `[-+]?\d+(\.\d+)?`, HasRelevance: true, Relevance: 0}
	str := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0
	literal := &Mode{ClassName: "literal", Begin: `\b(true|false|nil)\b`}
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

	global := &Mode{
		BeginKeywords: globalWords,
		Lexemes:       symbolRe,
		End:           `(\[|#|\d|"|:|\{|\)|\(|$)`,
		Contains: append([]*Mode{
			{ClassName: "title", Begin: symbolRe, HasRelevance: true, Relevance: 0, ExcludeEnd: true, EndsParent: true},
		}, defaultContains...),
	}

	list.Contains = []*Mode{CommentMode(`comment`, `\B|\b`), global, name, body}
	body.Contains = defaultContains
	collection.Contains = defaultContains
	hintCol.Contains = []*Mode{collection}

	return &Language{
		Name:    "Clojure",
		Aliases: []string{"clj"},
		Mode: Mode{
			Illegal: `\S`,
			Contains: []*Mode{
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
