package highlight

func init() { RegisterLanguage("qml", QML()) }

// QML returns the QML language definition (a port of languages/qml.js).
func QML() *Language {
	const qmlIdentRe = `[a-zA-Z_][a-zA-Z0-9\._]*`
	const jsIdentRe = `[A-Za-z$_][0-9A-Za-z$_]*`

	property := &Mode{
		ClassName: "keyword",
		Begin:     `\bproperty\b`,
		Starts: &Mode{
			ClassName: "string",
			End:       `(:|=|;|,|//|/\*|$)`,
			ReturnEnd: true,
		},
	}
	signalMode := &Mode{
		ClassName: "keyword",
		Begin:     `\bsignal\b`,
		Starts: &Mode{
			ClassName: "string",
			End:       `(\(|:|=|;|,|//|/\*|$)`,
			ReturnEnd: true,
		},
	}
	idID := &Mode{
		ClassName: "attribute",
		Begin:     `\bid\s*:`,
		Starts:    &Mode{ClassName: "string", End: qmlIdentRe, ReturnEnd: false},
	}
	qmlAttribute := &Mode{
		Begin:        qmlIdentRe + `\s*:`,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{
				ClassName:    "attribute",
				Begin:        qmlIdentRe,
				End:          `\s*:`,
				ExcludeEnd:   true,
				HasRelevance: true,
				Relevance:    0,
			},
		},
	}
	qmlObject := &Mode{
		Begin:        qmlIdentRe + `\s*\{`,
		End:          `\{`,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{Variant(TitleMode, func(m *Mode) { m.Begin = qmlIdentRe })},
	}

	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `\b(0[bB][01]+)` }),
		Variant(number, func(m *Mode) { m.Begin = `\b(0[oO][0-7]+)` }),
		Variant(number, func(m *Mode) { m.Begin = CNumberRe }),
	}

	valueContainer := &Mode{
		Begin:        `(` + arcadeReStartersRe + `|\b(case|return|throw)\b)\s*`,
		Keywords:     Keywords{"keyword": "return throw case"},
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			RegexpMode,
			{
				Begin:        `<`,
				End:          `>\s*[);\]]`,
				HasRelevance: true,
				Relevance:    0,
				SubLanguage:  []string{"xml"},
			},
		},
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function",
		End:           `\{`,
		ExcludeEnd:    true,
		Illegal:       `\[|%`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe }),
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Contains:     []*Mode{CLineCommentMode, CBlockCommentMode},
			},
		},
	}

	return &Language{
		Name:    "QML",
		Aliases: []string{"qt"},
		Keywords: Keywords{
			"keyword": "in of on if for while finally var new function do return void else break catch instanceof with throw " +
				"case default try this switch continue typeof delete let yield const export super debugger as async " +
				"await import",
			"literal": "true false null undefined NaN Infinity",
			"built_in": "eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent " +
				"escape unescape Object Function Boolean Error EvalError InternalError RangeError ReferenceError " +
				"StopIteration SyntaxError TypeError URIError Number Math Date String RegExp Array Float32Array " +
				"Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Uint8Array Uint8ClampedArray " +
				"ArrayBuffer DataView JSON Intl arguments require module console window document Symbol Set Map " +
				"WeakSet WeakMap Proxy Reflect Behavior bool color coordinate date double enumeration font geocircle " +
				"georectangle geoshape int list matrix4x4 parent point quaternion real rect size string url variant " +
				"vector2d vector3d vector4d Promise",
		},
		Illegal: `#`,
		Contains: []*Mode{
			{ClassName: "meta", Begin: `^\s*['"]use (strict|asm)['"]`},
			AposStringMode,
			QuoteStringMode,
			{
				ClassName: "string",
				Begin:     "\x60",
				End:       "\x60",
				Contains: []*Mode{
					BackslashEscape,
					{ClassName: "subst", Begin: `\$\{`, End: `\}`},
				},
			},
			CLineCommentMode,
			CBlockCommentMode,
			number,
			valueContainer,
			signalMode,
			property,
			function,
			{Begin: `\.` + IdentRe, HasRelevance: true, Relevance: 0},
			idID,
			qmlAttribute,
			qmlObject,
		},
	}
}
