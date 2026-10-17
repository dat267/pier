package highlight

func init() { RegisterLanguage("php", PHP()) }

// PHP returns the PHP language definition (a port of languages/php.js).
func PHP() *Language {
	// The reference's `\$+[a-zA-Z_\x7f-\xff][\w\x7f-\xff]*(?![A-Za-z0-9])(?![$])`
	// lookaheads become a post-match guard.
	variable := &Mode{
		ClassName: "variable",
		Begin:     "$+[a-zA-Z_\\x7f-\\xff][a-zA-Z0-9_\\x7f-\\xff]*",
	}
	variable.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next < len(m.input) {
			b := m.input[next]
			if isWordByte(b) || b == '$' {
				resp.IgnoreMatch()
			}
		}
	}

	preprocessor := &Mode{
		ClassName: "meta",
		Variants: []*Mode{
			{ClassName: "meta", Begin: `<\?php`, HasRelevance: true, Relevance: 10},
			{ClassName: "meta", Begin: `<\?[=]?`},
			{ClassName: "meta", Begin: `\?>`},
		},
	}
	subst := &Mode{
		ClassName: "subst",
		Variants: []*Mode{
			{ClassName: "subst", Begin: `\$\w+`},
			{ClassName: "subst", Begin: `\{\$`, End: `\}`},
		},
	}
	singleQuoted := Variant(AposStringMode, func(m *Mode) { m.Illegal = "" })
	doubleQuoted := Variant(QuoteStringMode, func(m *Mode) {
		m.Illegal = ""
		m.Contains = []*Mode{BackslashEscape, subst}
	})
	heredoc := EndSameAsBegin(&Mode{
		Begin:    `<<<[ \t]*(\w+)\n`,
		End:      `[ \t]*(\w+)\b`,
		Contains: []*Mode{BackslashEscape, subst},
	})
	stringMode := &Mode{
		ClassName: "string",
		Contains:  []*Mode{BackslashEscape, preprocessor},
		Variants: []*Mode{
			Variant(singleQuoted, func(m *Mode) { m.Begin = `b'`; m.End = `'` }),
			Variant(doubleQuoted, func(m *Mode) { m.Begin = `b"`; m.End = `"` }),
			Variant(doubleQuoted, func(*Mode) {}),
			Variant(singleQuoted, func(*Mode) {}),
			heredoc,
		},
	}
	number := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `\b0b[01]+(?:_[01]+)*\b`},
			{ClassName: "number", Begin: `\b0o[0-7]+(?:_[0-7]+)*\b`},
			{ClassName: "number", Begin: `\b0x[\da-f]+(?:_[\da-f]+)*\b`},
			{ClassName: "number", Begin: `(?:\b\d+(?:_\d+)*(\.(?:\d+(?:_\d+)*))?|\B\.\d+)(?:e[+-]?\d+)?`},
		},
	})

	keywords := Keywords{
		"keyword": "__CLASS__ __DIR__ __FILE__ __FUNCTION__ __LINE__ __METHOD__ __NAMESPACE__ __TRAIT__ " +
			"die echo exit include include_once print require require_once " +
			"array abstract and as binary bool boolean break callable case catch class clone const continue declare " +
			"default do double else elseif empty enddeclare endfor endforeach endif endswitch endwhile enum eval extends " +
			"final finally float for foreach from global goto if implements instanceof insteadof int integer interface " +
			"isset iterable list match|0 mixed new object or private protected public real return string switch throw trait " +
			"try unset use var void while xor yield",
		"literal": "false null true",
		"built_in": "Error|0 " +
			"AppendIterator ArgumentCountError ArithmeticError ArrayIterator ArrayObject AssertionError BadFunctionCallException BadMethodCallException CachingIterator CallbackFilterIterator CompileError Countable DirectoryIterator DivisionByZeroError DomainException EmptyIterator ErrorException Exception FilesystemIterator FilterIterator GlobIterator InfiniteIterator InvalidArgumentException IteratorIterator LengthException LimitIterator LogicException MultipleIterator NoRewindIterator OutOfBoundsException OutOfRangeException OuterIterator OverflowException ParentIterator ParseError RangeException RecursiveArrayIterator RecursiveCachingIterator RecursiveCallbackFilterIterator RecursiveDirectoryIterator RecursiveFilterIterator RecursiveIterator RecursiveIteratorIterator RecursiveRegexIterator RecursiveTreeIterator RegexIterator RuntimeException SeekableIterator SplDoublyLinkedList SplFileInfo SplFileObject SplFixedArray SplHeap SplMaxHeap SplMinHeap SplObjectStorage SplObserver SplObserver SplPriorityQueue SplQueue SplStack SplSubject SplSubject SplTempFileObject TypeError UnderflowException UnexpectedValueException UnhandledMatchError " +
			"ArrayAccess Closure Generator Iterator IteratorAggregate Serializable Stringable Throwable Traversable WeakReference WeakMap " +
			"Directory __PHP_Incomplete_Class parent php_user_filter self static stdClass",
	}

	lineComment := commentMode(`//`, `$`)
	lineComment.Contains = []*Mode{preprocessor, PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`})}

	blockComment := commentMode(`/\*`, `\*/`)
	blockComment.Contains = append([]*Mode{{ClassName: "doctag", Begin: `@[A-Za-z]+`}}, PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}))

	haltComment := &Mode{
		Begin:          `__halt_compiler.+?;`,
		EndsWithParent: true,
		Keywords:       Keywords{"keyword": "__halt_compiler"},
	}

	functionMode := Zero(Mode{
		ClassName:     "function",
		BeginKeywords: "fn function",
		End:           `[;{]`,
		ExcludeEnd:    true,
		Illegal:       `[$%\\[]`,
		Contains: []*Mode{
			{BeginKeywords: "use"},
			UnderscoreTitleMode,
			{Begin: `=>`, EndsParent: true},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Keywords:     keywords,
				Contains: []*Mode{
					Self,
					variable,
					CBlockCommentMode,
					stringMode,
					number,
				},
			},
		},
	})
	classMode := Zero(Mode{
		Variants: []*Mode{
			{ClassName: "class", BeginKeywords: "enum", Illegal: `[($"]`},
			{ClassName: "class", BeginKeywords: "class interface trait", Illegal: `[:($"]`},
		},
		End:        `\{`,
		ExcludeEnd: true,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	})
	namespaceMode := Zero(Mode{
		BeginKeywords: "namespace",
		End:           `;`,
		Illegal:       `[.']`,
		Contains:      []*Mode{UnderscoreTitleMode},
	})
	useMode := Zero(Mode{
		BeginKeywords: "use",
		End:           `;`,
		Contains:      []*Mode{UnderscoreTitleMode},
	})

	return &Language{
		Name:            "PHP",
		Aliases:         []string{"php3", "php4", "php5", "php6", "php7", "php8"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: keywords,
			Contains: []*Mode{
				HashCommentMode,
				lineComment,
				blockComment,
				haltComment,
				preprocessor,
				{ClassName: "keyword", Begin: `\$this\b`},
				variable,
				{Begin: `(::|->)+[a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*`},
				functionMode,
				classMode,
				namespaceMode,
				useMode,
				stringMode,
				number,
			},
		},
	}
}
