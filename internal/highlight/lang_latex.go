package highlight

func init() { RegisterLanguage("latex", Latex()) }

// Latex returns the LaTeX language definition (a port of languages/latex.js).
func Latex() *Language {
	return &Language{
		Name:            "LaTeX",
		Aliases:         []string{"tex"},
		CaseInsensitive: false,
		Contains: []*Mode{
			{
				Begin:              "\\\\verb",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\verb"},
				Starts: &Mode{
					// VERBATIM_DELIMITED_EQUAL: the text between the delimiters is
					// the string and the delimiters themselves are plain. Both
					// delimiters are the same single character, so EndSameAsBegin
					// ignores an end match that is not the opening delimiter.
					Contains: []*Mode{EndSameAsBegin(&Mode{
						ClassName:    "string",
						Begin:        "(.|\\r?\\n)",
						End:          "(.|\\r?\\n)",
						ExcludeBegin: true,
						ExcludeEnd:   true,
						EndsParent:   true,
					})},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\lstinline",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\lstinline"},
				Starts: &Mode{
					// VERBATIM_DELIMITED_EQUAL: the text between the delimiters is
					// the string and the delimiters themselves are plain. Both
					// delimiters are the same single character, so EndSameAsBegin
					// ignores an end match that is not the opening delimiter.
					Contains: []*Mode{EndSameAsBegin(&Mode{
						ClassName:    "string",
						Begin:        "(.|\\r?\\n)",
						End:          "(.|\\r?\\n)",
						ExcludeBegin: true,
						ExcludeEnd:   true,
						EndsParent:   true,
					})},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\mint",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\mint"},
				Starts: &Mode{
					Starts: &Mode{
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							Contains: []*Mode{{
								ClassName:    "string",
								Begin:        "(.|\\r?\\n)",
								End:          "(.|\\r?\\n)",
								ExcludeBegin: true,
								ExcludeEnd:   true,
								EndsParent:   true,
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							EndsParent:   true,
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:        "\\{",
								End:          "\\}",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Match: "function match() { [native code] }",
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\s+",
						HasRelevance: true, Relevance: 0,
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\mintinline",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\mintinline"},
				Starts: &Mode{
					Starts: &Mode{
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							Contains: []*Mode{{
								Begin:        "\\{",
								HasRelevance: true, Relevance: 0,
								Starts: &Mode{
									EndsParent: true,
									Contains: []*Mode{{
										ClassName:  "string",
										End:        "\\}",
										ExcludeEnd: true,
										EndsParent: true,
										Contains: []*Mode{{
											Begin:        "\\{",
											End:          "\\}",
											HasRelevance: true, Relevance: 0,
											Contains: []*Mode{{
												Match: "function match() { [native code] }",
											}},
										}},
									}},
								},
							}, {
								ClassName:    "string",
								Begin:        "(.|\\r?\\n)",
								End:          "(.|\\r?\\n)",
								ExcludeBegin: true,
								ExcludeEnd:   true,
								EndsParent:   true,
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							EndsParent:   true,
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:        "\\{",
								End:          "\\}",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Match: "function match() { [native code] }",
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\s+",
						HasRelevance: true, Relevance: 0,
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\url",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\url"},
				Starts: &Mode{
					Contains: []*Mode{{
						Begin:        "\\{",
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							EndsParent: true,
							Contains: []*Mode{{
								ClassName:  "link",
								End:        "\\}",
								ExcludeEnd: true,
								EndsParent: true,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}},
								}},
							}},
						},
					}, {
						Begin:        "\\{",
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							EndsParent: true,
							Contains: []*Mode{{
								ClassName:  "link",
								End:        "\\}",
								ExcludeEnd: true,
								EndsParent: true,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}},
								}},
							}},
						},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\hyperref",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\hyperref"},
				Starts: &Mode{
					Contains: []*Mode{{
						Begin:        "\\{",
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							EndsParent: true,
							Contains: []*Mode{{
								ClassName:  "link",
								End:        "\\}",
								ExcludeEnd: true,
								EndsParent: true,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}},
								}},
							}},
						},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:              "\\\\href",
				BeginNotFollowedBy: "[a-zA-Z@:_]",
				HasRelevance:       true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\href"},
				Starts: &Mode{
					Starts: &Mode{
						HasRelevance: true, Relevance: 0,
						Starts: &Mode{
							Contains: []*Mode{{
								Begin:        "\\{",
								HasRelevance: true, Relevance: 0,
								Starts: &Mode{
									EndsParent: true,
									Contains: []*Mode{{
										ClassName:  "link",
										End:        "\\}",
										ExcludeEnd: true,
										EndsParent: true,
										Contains: []*Mode{{
											Begin:        "\\{",
											End:          "\\}",
											HasRelevance: true, Relevance: 0,
											Contains: []*Mode{{
												Match: "function match() { [native code] }",
											}},
										}},
									}},
								},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\[",
							End:          "\\]",
							EndsParent:   true,
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:        "\\{",
								End:          "\\}",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Match: "function match() { [native code] }",
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\s+",
						HasRelevance: true, Relevance: 0,
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{verbatim\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						ClassName:  "string",
						End:        "\\\\end\\{verbatim\\}",
						ExcludeEnd: true,
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{filecontents\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{filecontents\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\{",
								End:          "\\}",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{Verbatim\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{Verbatim\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{BVerbatim\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{BVerbatim\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{LVerbatim\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{LVerbatim\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{verbatim\\*\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						ClassName:  "string",
						End:        "\\\\end\\{verbatim\\*\\}",
						ExcludeEnd: true,
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{filecontents\\*\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{filecontents\\*\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\{",
								End:          "\\}",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{Verbatim\\*\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{Verbatim\\*\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{BVerbatim\\*\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{BVerbatim\\*\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{LVerbatim\\*\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								ClassName:  "string",
								End:        "\\\\end\\{LVerbatim\\*\\}",
								ExcludeEnd: true,
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				Begin:           "\\\\begin",
				BeginFollowedBy: "[ 	]*(\\r?\\n[ 	]*)?\\{minted\\}",
				HasRelevance:    true, Relevance: 0,
				Keywords: Keywords{"$pattern": "\\\\[a-zA-Z]+", "keyword": "\\begin"},
				Starts: &Mode{
					HasRelevance: true, Relevance: 0,
					Starts: &Mode{
						Starts: &Mode{
							HasRelevance: true, Relevance: 0,
							Starts: &Mode{
								Starts: &Mode{
									HasRelevance: true, Relevance: 0,
									Starts: &Mode{
										ClassName:  "string",
										End:        "\\\\end\\{minted\\}",
										ExcludeEnd: true,
									},
									Contains: []*Mode{{
										Begin:        "\\{",
										End:          "\\}",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:        "\\{",
											End:          "\\}",
											HasRelevance: true, Relevance: 0,
											Contains: []*Mode{{
												Match: "function match() { [native code] }",
											}, {
												ClassName:    "keyword",
												Begin:        "\\\\",
												HasRelevance: true, Relevance: 0,
												Contains: []*Mode{{
													Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
													BeginNotFollowedBy: "[a-zA-Z@:_]",
													EndsParent:         true,
												}, {
													Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
													BeginNotFollowedBy: "[a-zA-Z:_]",
													EndsParent:         true,
												}, {
													EndsParent: true,
													Variants: []*Mode{{
														Begin:      "\\^{6}[0-9a-f]{6}",
														EndsParent: true,
													}, {
														Begin:      "\\^{5}[0-9a-f]{5}",
														EndsParent: true,
													}, {
														Begin:      "\\^{4}[0-9a-f]{4}",
														EndsParent: true,
													}, {
														Begin:      "\\^{3}[0-9a-f]{3}",
														EndsParent: true,
													}, {
														Begin:      "\\^{2}[0-9a-f]{2}",
														EndsParent: true,
													}, {
														Begin:      "\\^{2}[\\u0000-\\u007f]",
														EndsParent: true,
													}},
												}, {
													EndsParent:   true,
													HasRelevance: true, Relevance: 0,
													Variants: []*Mode{{
														Begin:        "[a-zA-Z@]+",
														EndsParent:   true,
														HasRelevance: true, Relevance: 0,
													}, {
														Begin:        "[^a-zA-Z@]?",
														EndsParent:   true,
														HasRelevance: true, Relevance: 0,
													}},
												}},
											}, {
												ClassName:    "params",
												Begin:        "#+\\d?",
												HasRelevance: true, Relevance: 0,
											}, {
												Variants: []*Mode{{
													Begin: "\\^{6}[0-9a-f]{6}",
												}, {
													Begin: "\\^{5}[0-9a-f]{5}",
												}, {
													Begin: "\\^{4}[0-9a-f]{4}",
												}, {
													Begin: "\\^{3}[0-9a-f]{3}",
												}, {
													Begin: "\\^{2}[0-9a-f]{2}",
												}, {
													Begin: "\\^{2}[\\u0000-\\u007f]",
												}},
											}, {
												ClassName:    "built_in",
												Begin:        "[$&^_]",
												HasRelevance: true, Relevance: 0,
											}, {
												ClassName:    "meta",
												Begin:        "% !TeX",
												End:          "$",
												HasRelevance: true, Relevance: 10,
											}, {
												ClassName:    "comment",
												Begin:        "%",
												End:          "$",
												HasRelevance: true, Relevance: 0,
												Contains: []*Mode{{
													Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
												}, {
													ClassName:    "doctag",
													Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
													HasRelevance: true, Relevance: 0,
												}},
											}},
										}, {
											ClassName:    "keyword",
											Begin:        "\\\\",
											HasRelevance: true, Relevance: 0,
											Contains: []*Mode{{
												Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
												BeginNotFollowedBy: "[a-zA-Z@:_]",
												EndsParent:         true,
											}, {
												Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
												BeginNotFollowedBy: "[a-zA-Z:_]",
												EndsParent:         true,
											}, {
												EndsParent: true,
												Variants: []*Mode{{
													Begin:      "\\^{6}[0-9a-f]{6}",
													EndsParent: true,
												}, {
													Begin:      "\\^{5}[0-9a-f]{5}",
													EndsParent: true,
												}, {
													Begin:      "\\^{4}[0-9a-f]{4}",
													EndsParent: true,
												}, {
													Begin:      "\\^{3}[0-9a-f]{3}",
													EndsParent: true,
												}, {
													Begin:      "\\^{2}[0-9a-f]{2}",
													EndsParent: true,
												}, {
													Begin:      "\\^{2}[\\u0000-\\u007f]",
													EndsParent: true,
												}},
											}, {
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
												Variants: []*Mode{{
													Begin:        "[a-zA-Z@]+",
													EndsParent:   true,
													HasRelevance: true, Relevance: 0,
												}, {
													Begin:        "[^a-zA-Z@]?",
													EndsParent:   true,
													HasRelevance: true, Relevance: 0,
												}},
											}},
										}, {
											ClassName:    "params",
											Begin:        "#+\\d?",
											HasRelevance: true, Relevance: 0,
										}, {
											Variants: []*Mode{{
												Begin: "\\^{6}[0-9a-f]{6}",
											}, {
												Begin: "\\^{5}[0-9a-f]{5}",
											}, {
												Begin: "\\^{4}[0-9a-f]{4}",
											}, {
												Begin: "\\^{3}[0-9a-f]{3}",
											}, {
												Begin: "\\^{2}[0-9a-f]{2}",
											}, {
												Begin: "\\^{2}[\\u0000-\\u007f]",
											}},
										}, {
											ClassName:    "built_in",
											Begin:        "[$&^_]",
											HasRelevance: true, Relevance: 0,
										}, {
											ClassName:    "meta",
											Begin:        "% !TeX",
											End:          "$",
											HasRelevance: true, Relevance: 10,
										}, {
											ClassName:    "comment",
											Begin:        "%",
											End:          "$",
											HasRelevance: true, Relevance: 0,
											Contains: []*Mode{{
												Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
											}, {
												ClassName:    "doctag",
												Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}},
								},
								Contains: []*Mode{{
									Begin:        "\\s+",
									HasRelevance: true, Relevance: 0,
								}},
							},
							Contains: []*Mode{{
								Begin:        "\\[",
								End:          "\\]",
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:        "\\{",
									End:          "\\}",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Match: "function match() { [native code] }",
									}, {
										ClassName:    "keyword",
										Begin:        "\\\\",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
											BeginNotFollowedBy: "[a-zA-Z@:_]",
											EndsParent:         true,
										}, {
											Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
											BeginNotFollowedBy: "[a-zA-Z:_]",
											EndsParent:         true,
										}, {
											EndsParent: true,
											Variants: []*Mode{{
												Begin:      "\\^{6}[0-9a-f]{6}",
												EndsParent: true,
											}, {
												Begin:      "\\^{5}[0-9a-f]{5}",
												EndsParent: true,
											}, {
												Begin:      "\\^{4}[0-9a-f]{4}",
												EndsParent: true,
											}, {
												Begin:      "\\^{3}[0-9a-f]{3}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[0-9a-f]{2}",
												EndsParent: true,
											}, {
												Begin:      "\\^{2}[\\u0000-\\u007f]",
												EndsParent: true,
											}},
										}, {
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
											Variants: []*Mode{{
												Begin:        "[a-zA-Z@]+",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}, {
												Begin:        "[^a-zA-Z@]?",
												EndsParent:   true,
												HasRelevance: true, Relevance: 0,
											}},
										}},
									}, {
										ClassName:    "params",
										Begin:        "#+\\d?",
										HasRelevance: true, Relevance: 0,
									}, {
										Variants: []*Mode{{
											Begin: "\\^{6}[0-9a-f]{6}",
										}, {
											Begin: "\\^{5}[0-9a-f]{5}",
										}, {
											Begin: "\\^{4}[0-9a-f]{4}",
										}, {
											Begin: "\\^{3}[0-9a-f]{3}",
										}, {
											Begin: "\\^{2}[0-9a-f]{2}",
										}, {
											Begin: "\\^{2}[\\u0000-\\u007f]",
										}},
									}, {
										ClassName:    "built_in",
										Begin:        "[$&^_]",
										HasRelevance: true, Relevance: 0,
									}, {
										ClassName:    "meta",
										Begin:        "% !TeX",
										End:          "$",
										HasRelevance: true, Relevance: 10,
									}, {
										ClassName:    "comment",
										Begin:        "%",
										End:          "$",
										HasRelevance: true, Relevance: 0,
										Contains: []*Mode{{
											Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
										}, {
											ClassName:    "doctag",
											Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "keyword",
									Begin:        "\\\\",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
										BeginNotFollowedBy: "[a-zA-Z@:_]",
										EndsParent:         true,
									}, {
										Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
										BeginNotFollowedBy: "[a-zA-Z:_]",
										EndsParent:         true,
									}, {
										EndsParent: true,
										Variants: []*Mode{{
											Begin:      "\\^{6}[0-9a-f]{6}",
											EndsParent: true,
										}, {
											Begin:      "\\^{5}[0-9a-f]{5}",
											EndsParent: true,
										}, {
											Begin:      "\\^{4}[0-9a-f]{4}",
											EndsParent: true,
										}, {
											Begin:      "\\^{3}[0-9a-f]{3}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[0-9a-f]{2}",
											EndsParent: true,
										}, {
											Begin:      "\\^{2}[\\u0000-\\u007f]",
											EndsParent: true,
										}},
									}, {
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
										Variants: []*Mode{{
											Begin:        "[a-zA-Z@]+",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}, {
											Begin:        "[^a-zA-Z@]?",
											EndsParent:   true,
											HasRelevance: true, Relevance: 0,
										}},
									}},
								}, {
									ClassName:    "params",
									Begin:        "#+\\d?",
									HasRelevance: true, Relevance: 0,
								}, {
									Variants: []*Mode{{
										Begin: "\\^{6}[0-9a-f]{6}",
									}, {
										Begin: "\\^{5}[0-9a-f]{5}",
									}, {
										Begin: "\\^{4}[0-9a-f]{4}",
									}, {
										Begin: "\\^{3}[0-9a-f]{3}",
									}, {
										Begin: "\\^{2}[0-9a-f]{2}",
									}, {
										Begin: "\\^{2}[\\u0000-\\u007f]",
									}},
								}, {
									ClassName:    "built_in",
									Begin:        "[$&^_]",
									HasRelevance: true, Relevance: 0,
								}, {
									ClassName:    "meta",
									Begin:        "% !TeX",
									End:          "$",
									HasRelevance: true, Relevance: 10,
								}, {
									ClassName:    "comment",
									Begin:        "%",
									End:          "$",
									HasRelevance: true, Relevance: 0,
									Contains: []*Mode{{
										Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
									}, {
										ClassName:    "doctag",
										Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}},
						},
						Contains: []*Mode{{
							Begin:        "\\s+",
							HasRelevance: true, Relevance: 0,
						}},
					},
					Contains: []*Mode{{
						Begin:        "\\{",
						End:          "\\}",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
						Contains: []*Mode{{
							Begin:        "\\{",
							End:          "\\}",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Match: "function match() { [native code] }",
							}, {
								ClassName:    "keyword",
								Begin:        "\\\\",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
									BeginNotFollowedBy: "[a-zA-Z@:_]",
									EndsParent:         true,
								}, {
									Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
									BeginNotFollowedBy: "[a-zA-Z:_]",
									EndsParent:         true,
								}, {
									EndsParent: true,
									Variants: []*Mode{{
										Begin:      "\\^{6}[0-9a-f]{6}",
										EndsParent: true,
									}, {
										Begin:      "\\^{5}[0-9a-f]{5}",
										EndsParent: true,
									}, {
										Begin:      "\\^{4}[0-9a-f]{4}",
										EndsParent: true,
									}, {
										Begin:      "\\^{3}[0-9a-f]{3}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[0-9a-f]{2}",
										EndsParent: true,
									}, {
										Begin:      "\\^{2}[\\u0000-\\u007f]",
										EndsParent: true,
									}},
								}, {
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
									Variants: []*Mode{{
										Begin:        "[a-zA-Z@]+",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}, {
										Begin:        "[^a-zA-Z@]?",
										EndsParent:   true,
										HasRelevance: true, Relevance: 0,
									}},
								}},
							}, {
								ClassName:    "params",
								Begin:        "#+\\d?",
								HasRelevance: true, Relevance: 0,
							}, {
								Variants: []*Mode{{
									Begin: "\\^{6}[0-9a-f]{6}",
								}, {
									Begin: "\\^{5}[0-9a-f]{5}",
								}, {
									Begin: "\\^{4}[0-9a-f]{4}",
								}, {
									Begin: "\\^{3}[0-9a-f]{3}",
								}, {
									Begin: "\\^{2}[0-9a-f]{2}",
								}, {
									Begin: "\\^{2}[\\u0000-\\u007f]",
								}},
							}, {
								ClassName:    "built_in",
								Begin:        "[$&^_]",
								HasRelevance: true, Relevance: 0,
							}, {
								ClassName:    "meta",
								Begin:        "% !TeX",
								End:          "$",
								HasRelevance: true, Relevance: 10,
							}, {
								ClassName:    "comment",
								Begin:        "%",
								End:          "$",
								HasRelevance: true, Relevance: 0,
								Contains: []*Mode{{
									Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
								}, {
									ClassName:    "doctag",
									Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "keyword",
							Begin:        "\\\\",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
								BeginNotFollowedBy: "[a-zA-Z@:_]",
								EndsParent:         true,
							}, {
								Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
								BeginNotFollowedBy: "[a-zA-Z:_]",
								EndsParent:         true,
							}, {
								EndsParent: true,
								Variants: []*Mode{{
									Begin:      "\\^{6}[0-9a-f]{6}",
									EndsParent: true,
								}, {
									Begin:      "\\^{5}[0-9a-f]{5}",
									EndsParent: true,
								}, {
									Begin:      "\\^{4}[0-9a-f]{4}",
									EndsParent: true,
								}, {
									Begin:      "\\^{3}[0-9a-f]{3}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[0-9a-f]{2}",
									EndsParent: true,
								}, {
									Begin:      "\\^{2}[\\u0000-\\u007f]",
									EndsParent: true,
								}},
							}, {
								EndsParent:   true,
								HasRelevance: true, Relevance: 0,
								Variants: []*Mode{{
									Begin:        "[a-zA-Z@]+",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}, {
									Begin:        "[^a-zA-Z@]?",
									EndsParent:   true,
									HasRelevance: true, Relevance: 0,
								}},
							}},
						}, {
							ClassName:    "params",
							Begin:        "#+\\d?",
							HasRelevance: true, Relevance: 0,
						}, {
							Variants: []*Mode{{
								Begin: "\\^{6}[0-9a-f]{6}",
							}, {
								Begin: "\\^{5}[0-9a-f]{5}",
							}, {
								Begin: "\\^{4}[0-9a-f]{4}",
							}, {
								Begin: "\\^{3}[0-9a-f]{3}",
							}, {
								Begin: "\\^{2}[0-9a-f]{2}",
							}, {
								Begin: "\\^{2}[\\u0000-\\u007f]",
							}},
						}, {
							ClassName:    "built_in",
							Begin:        "[$&^_]",
							HasRelevance: true, Relevance: 0,
						}, {
							ClassName:    "meta",
							Begin:        "% !TeX",
							End:          "$",
							HasRelevance: true, Relevance: 10,
						}, {
							ClassName:    "comment",
							Begin:        "%",
							End:          "$",
							HasRelevance: true, Relevance: 0,
							Contains: []*Mode{{
								Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
							}, {
								ClassName:    "doctag",
								Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
								HasRelevance: true, Relevance: 0,
							}},
						}},
					}},
				},
				Contains: []*Mode{{
					Begin:        "\\s+",
					HasRelevance: true, Relevance: 0,
				}},
			},
			{
				ClassName:    "keyword",
				Begin:        "\\\\",
				HasRelevance: true, Relevance: 0,
				Contains: []*Mode{{
					Begin:              "(?:(?:NeedsTeXFormat|RequirePackage|GetIdInfo)|Provides(?:Expl)?(?:Package|Class|File)|(?:DeclareOption|ProcessOptions)|(?:documentclass|usepackage|input|include)|makeat(?:letter|other)|ExplSyntax(?:On|Off)|(?:new|renew|provide)?command|(?:re)newenvironment|(?:New|Renew|Provide|Declare)(?:Expandable)?DocumentCommand|(?:New|Renew|Provide|Declare)DocumentEnvironment|(?:(?:e|g|x)?def|let)|(?:begin|end)|(?:part|chapter|(?:sub){0,2}section|(?:sub)?paragraph)|caption|(?:label|(?:eq|page|name)?ref|(?:paren|foot|super)?cite)|(?:alpha|beta|[Gg]amma|[Dd]elta|(?:var)?epsilon|zeta|eta|[Tt]heta|vartheta)|(?:iota|(?:var)?kappa|[Ll]ambda|mu|nu|[Xx]i|[Pp]i|varpi|(?:var)rho)|(?:[Ss]igma|varsigma|tau|[Uu]psilon|[Pp]hi|varphi|chi|[Pp]si|[Oo]mega)|(?:frac|sum|prod|lim|infty|times|sqrt|leq|geq|left|right|middle|[bB]igg?)|(?:[lr]angle|q?quad|[lcvdi]?dots|d?dot|hat|tilde|bar))",
					BeginNotFollowedBy: "[a-zA-Z@:_]",
					EndsParent:         true,
				}, {
					Begin:              "(?:(?:__)?[a-zA-Z]{2,}_[a-zA-Z](?:_?[a-zA-Z])+:[a-zA-Z]*|[lgc]__?[a-zA-Z](?:_?[a-zA-Z])*_[a-zA-Z]{2,}|[qs]__?[a-zA-Z](?:_?[a-zA-Z])+|use(?:_i)?:[a-zA-Z]*|(?:else|fi|or):|(?:if|cs|exp):w|(?:hbox|vbox):n|::[a-zA-Z]_unbraced|::[a-zA-Z:])",
					BeginNotFollowedBy: "[a-zA-Z:_]",
					EndsParent:         true,
				}, {
					EndsParent: true,
					Variants: []*Mode{{
						Begin:      "\\^{6}[0-9a-f]{6}",
						EndsParent: true,
					}, {
						Begin:      "\\^{5}[0-9a-f]{5}",
						EndsParent: true,
					}, {
						Begin:      "\\^{4}[0-9a-f]{4}",
						EndsParent: true,
					}, {
						Begin:      "\\^{3}[0-9a-f]{3}",
						EndsParent: true,
					}, {
						Begin:      "\\^{2}[0-9a-f]{2}",
						EndsParent: true,
					}, {
						Begin:      "\\^{2}[\\u0000-\\u007f]",
						EndsParent: true,
					}},
				}, {
					EndsParent:   true,
					HasRelevance: true, Relevance: 0,
					Variants: []*Mode{{
						Begin:        "[a-zA-Z@]+",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
					}, {
						Begin:        "[^a-zA-Z@]?",
						EndsParent:   true,
						HasRelevance: true, Relevance: 0,
					}},
				}},
			},
			{
				ClassName:    "params",
				Begin:        "#+\\d?",
				HasRelevance: true, Relevance: 0,
			},
			{
				Variants: []*Mode{{
					Begin: "\\^{6}[0-9a-f]{6}",
				}, {
					Begin: "\\^{5}[0-9a-f]{5}",
				}, {
					Begin: "\\^{4}[0-9a-f]{4}",
				}, {
					Begin: "\\^{3}[0-9a-f]{3}",
				}, {
					Begin: "\\^{2}[0-9a-f]{2}",
				}, {
					Begin: "\\^{2}[\\u0000-\\u007f]",
				}},
			},
			{
				ClassName:    "built_in",
				Begin:        "[$&^_]",
				HasRelevance: true, Relevance: 0,
			},
			{
				ClassName:    "meta",
				Begin:        "% !TeX",
				End:          "$",
				HasRelevance: true, Relevance: 10,
			},
			{
				ClassName:    "comment",
				Begin:        "%",
				End:          "$",
				HasRelevance: true, Relevance: 0,
				Contains: []*Mode{{
					Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b",
				}, {
					ClassName:    "doctag",
					Begin:        "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):",
					HasRelevance: true, Relevance: 0,
				}},
			},
		},
	}
}
