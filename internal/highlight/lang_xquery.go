package highlight

func init() { RegisterLanguage("xquery", XQuery()) }

// XQuery returns the XQuery language definition (a port of languages/xquery.js).
func XQuery() *Language {
	builtIn := &Mode{ClassName: "built_in"}
	builtIn.Variants = []*Mode{
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\barray:`
			m.End = `(?:append|filter|flatten|fold-(?:left|right)|for-each(?:-pair)?|get|head|insert-before|join|put|remove|reverse|size|sort|subarray|tail)\b`
		}),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\bmap:`
			m.End = `(?:contains|entry|find|for-each|get|keys|merge|put|remove|size)\b`
		}),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\bmath:`
			m.End = `(?:a(?:cos|sin|tan[2]?)|cos|exp(?:10)?|log(?:10)?|pi|pow|sin|sqrt|tan)\b`
		}),
		Variant(builtIn, func(m *Mode) { m.Begin = `\bop:`; m.End = `\(`; m.ExcludeEnd = true }),
		Variant(builtIn, func(m *Mode) { m.Begin = `\bfn:`; m.End = `\(`; m.ExcludeEnd = true }),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `[^</$:'"-]\b(?:abs|accumulator-(?:after|before)|adjust-(?:date(?:Time)?|time)-to-timezone|analyze-string|apply|available-(?:environment-variables|system-properties)|avg|base-uri|boolean|ceiling|codepoints?-(?:equal|to-string)|collation-key|collection|compare|concat|contains(?:-token)?|copy-of|count|current(?:-)?(?:date(?:Time)?|time|group(?:ing-key)?|output-uri|merge-(?:group|key))?data|dateTime|days?-from-(?:date(?:Time)?|duration)|deep-equal|default-(?:collation|language)|distinct-values|document(?:-uri)?|doc(?:-available)?|element-(?:available|with-id)|empty|encode-for-uri|ends-with|environment-variable|error|escape-html-uri|exactly-one|exists|false|filter|floor|fold-(?:left|right)|for-each(?:-pair)?|format-(?:date(?:Time)?|time|integer|number)|function-(?:arity|available|lookup|name)|generate-id|has-children|head|hours-from-(?:dateTime|duration|time)|id(?:ref)?|implicit-timezone|in-scope-prefixes|index-of|innermost|insert-before|iri-to-uri|json-(?:doc|to-xml)|key|lang|last|load-xquery-module|local-name(?:-from-QName)?|(?:lower|upper)-case|matches|max|minutes-from-(?:dateTime|duration|time)|min|months?-from-(?:date(?:Time)?|duration)|name(?:space-uri-?(?:for-prefix|from-QName)?)?|nilled|node-name|normalize-(?:space|unicode)|not|number|one-or-more|outermost|parse-(?:ietf-date|json)|path|position|(?:prefix-from-)?QName|random-number-generator|regex-group|remove|replace|resolve-(?:QName|uri)|reverse|root|round(?:-half-to-even)?|seconds-from-(?:dateTime|duration|time)|snapshot|sort|starts-with|static-base-uri|stream-available|string-?(?:join|length|to-codepoints)?|subsequence|substring-?(?:after|before)?|sum|system-property|tail|timezone-from-(?:date(?:Time)?|time)|tokenize|trace|trans(?:form|late)|true|type-available|unordered|unparsed-(?:entity|text)?-?(?:public-id|uri|available|lines)?|uri-collection|xml-to-json|years?-from-(?:date(?:Time)?|duration)|zero-or-one)\b`
		}),
		Variant(builtIn, func(m *Mode) { m.Begin = `\blocal:`; m.End = `\(`; m.ExcludeEnd = true }),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\bzip:`
			m.End = `(?:zip-file|(?:xml|html|text|binary)-entry| (?:update-)?entries)\b`
		}),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\b(?:util|db|functx|app|xdmp|xmldb):`
			m.End = `\(`
			m.ExcludeEnd = true
		}),
	}

	title := &Mode{
		ClassName: "title",
		Begin:     `\bxquery version "[13]\.[01]"\s?(?:encoding ".+")?`,
		End:       `;`,
	}
	variable := &Mode{ClassName: "variable", Begin: `[$][\w\-:]+`}
	number := &Mode{
		ClassName:    "number",
		Begin:        `(\b0[0-7_]+)|(\b0x[0-9a-fA-F_]+)|(\b[1-9][0-9_]*(\.[0-9_]+)?)|[0_]\b`,
		HasRelevance: true,
		Relevance:    0,
	}
	stringBase := &Mode{ClassName: "string"}
	stringBase.Variants = []*Mode{
		Variant(stringBase, func(m *Mode) {
			m.Begin = `"`
			m.End = `"`
			m.Contains = []*Mode{{Begin: `""`, HasRelevance: true, Relevance: 0}}
		}),
		Variant(stringBase, func(m *Mode) {
			m.Begin = `'`
			m.End = `'`
			m.Contains = []*Mode{{Begin: `''`, HasRelevance: true, Relevance: 0}}
		}),
	}
	annotation := &Mode{ClassName: "meta", Begin: `%[\w\-:]+`}
	comment := &Mode{
		ClassName:    "comment",
		Begin:        `\(:`,
		End:          `:\)`,
		HasRelevance: true,
		Relevance:    10,
		Contains:     []*Mode{{ClassName: "doctag", Begin: `@\w+`}},
	}
	computed := &Mode{
		BeginKeywords: "element attribute comment document processing-instruction",
		End:           `\{`,
		ExcludeEnd:    true,
	}
	direct := &Mode{
		Begin:       `<([\w._:-]+)(\s+\S*=('|").*('|"))?>`,
		End:         `(\/[\w._:-]+>)`,
		SubLanguage: []string{"xml"},
		Contains: []*Mode{
			{Begin: `\{`, End: `\}`, SubLanguage: []string{"xquery"}},
			Self,
		},
	}

	return &Language{
		Name:            "XQuery",
		Aliases:         []string{"xpath", "xq"},
		CaseInsensitive: false,
		Keywords: Keywords{
			"$pattern": "[a-zA-Z$][a-zA-Z0-9_:-]*",
			"keyword": "module schema namespace boundary-space preserve no-preserve strip default collation base-uri " +
				"ordering context decimal-format decimal-separator copy-namespaces empty-sequence except " +
				"exponent-separator external grouping-separator inherit no-inherit lax minus-sign per-mille percent " +
				"schema-attribute schema-element strict unordered zero-digit declare import option function validate " +
				"variable for at in let where order group by return if then else tumbling sliding window start when " +
				"only end previous next stable ascending descending allowing empty greatest least some every " +
				"satisfies switch case typeswitch try catch and or to union intersect instance of treat as castable " +
				"cast map array delete insert into replace value rename copy modify update",
			"type": "item document-node node attribute document element comment namespace namespace-node " +
				"processing-instruction text construction xs:anyAtomicType xs:untypedAtomic xs:duration xs:time " +
				"xs:decimal xs:float xs:double xs:gYearMonth xs:gYear xs:gMonthDay xs:gMonth xs:gDay xs:boolean " +
				"xs:base64Binary xs:hexBinary xs:anyURI xs:QName xs:NOTATION xs:dateTime xs:dateTimeStamp xs:date " +
				"xs:string xs:normalizedString xs:token xs:language xs:NMTOKEN xs:Name xs:NCName xs:ID xs:IDREF " +
				"xs:ENTITY xs:integer xs:nonPositiveInteger xs:negativeInteger xs:long xs:int xs:short xs:byte " +
				"xs:nonNegativeInteger xs:unisignedLong xs:unsignedInt xs:unsignedShort xs:unsignedByte " +
				"xs:positiveInteger xs:yearMonthDuration xs:dayTimeDuration",
			"literal": "eq ne lt le gt ge is self:: child:: descendant:: descendant-or-self:: attribute:: following:: " +
				"following-sibling:: parent:: ancestor:: ancestor-or-self:: preceding:: preceding-sibling:: NaN",
		},
		Illegal: `(proc)|(abstract)|(extends)|(until)|(#)`,
		Contains: []*Mode{
			variable,
			builtIn,
			stringBase,
			number,
			comment,
			annotation,
			title,
			computed,
			direct,
		},
	}
}
