package highlight

func init() { RegisterLanguage("arcade", Arcade()) }

// arcadeReStartersRe is core.js's RE_STARTERS_RE.
const arcadeReStartersRe = `!|!=|!==|%|%=|&|&&|&=|\*|\*=|\+|\+=|,|-|-=|/=|/|:|;|<<|<<=|<=|<|===|==|=|>>>=|>>=|>=|>>>|>>|>|\?|\[|\{|\(|\^|\^=|\||\|=|\|\||~`

// Arcade returns the ArcGIS Arcade language definition (a port of
// languages/arcade.js).
func Arcade() *Language {
	const identRe = `[A-Za-z_][0-9A-Za-z_]*`
	keywords := Keywords{
		"keyword": "if for while var new function do return void else break",
		"literal": "BackSlash DoubleQuote false ForwardSlash Infinity NaN NewLine null PI SingleQuote Tab TextFormatting " +
			"true undefined",
		"built_in": "Abs Acos Angle Attachments Area AreaGeodetic Asin Atan Atan2 Average Bearing Boolean Buffer " +
			"BufferGeodetic Ceil Centroid Clip Console Constrain Contains Cos Count Crosses Cut Date DateAdd " +
			"DateDiff Day Decode DefaultValue Dictionary Difference Disjoint Distance DistanceGeodetic Distinct " +
			"DomainCode DomainName Equals Exp Extent Feature FeatureSet FeatureSetByAssociation FeatureSetById " +
			"FeatureSetByPortalItem FeatureSetByRelationshipName FeatureSetByTitle FeatureSetByUrl Filter First " +
			"Floor Geometry GroupBy Guid HasKey Hour IIf IndexOf Intersection Intersects IsEmpty IsNan " +
			"IsSelfIntersecting Length LengthGeodetic Log Max Mean Millisecond Min Minute Month " +
			"MultiPartToSinglePart Multipoint NextSequenceValue Now Number OrderBy Overlaps Point Polygon " +
			"Polyline Portal Pow Random Relate Reverse RingIsClockWise Round Second SetGeometry Sin Sort Sqrt " +
			"Stdev Sum SymmetricDifference Tan Text Timestamp Today ToLocal Top Touches ToUTC TrackCurrentTime " +
			"TrackGeometryWindow TrackIndex TrackStartTime TrackWindow TypeOf Union UrlEncode Variance Weekday " +
			"When Within Year ",
	}

	symbol := &Mode{
		ClassName: "symbol",
		Begin:     `\$[datastore|feature|layer|map|measure|sourcefeature|sourcelayer|targetfeature|targetlayer|value|view]+`,
	}

	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `\b(0[bB][01]+)` }),
		Variant(number, func(m *Mode) { m.Begin = `\b(0[oO][0-7]+)` }),
		Variant(number, func(m *Mode) { m.Begin = CNumberRe }),
	}

	subst := &Mode{ClassName: "subst", Begin: `\$\{`, End: `\}`, Keywords: keywords}
	templateString := &Mode{
		ClassName: "string",
		Begin:     "\x60",
		End:       "\x60",
		Contains:  []*Mode{BackslashEscape, subst},
	}
	subst.Contains = []*Mode{
		AposStringMode,
		QuoteStringMode,
		templateString,
		number,
		RegexpMode,
	}
	paramsContains := append([]*Mode{}, subst.Contains...)
	paramsContains = append(paramsContains, CBlockCommentMode, CLineCommentMode)

	attrContainer := &Mode{
		Begin:        `[{,]\s*`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{
				Begin:        identRe + `\s*:`,
				ReturnBegin:  true,
				HasRelevance: true,
				Relevance:    0,
				Contains:     []*Mode{{ClassName: "attr", Begin: identRe, HasRelevance: true, Relevance: 0}},
			},
		},
	}

	functionValue := &Mode{
		ClassName:   "function",
		Begin:       `(\(.*?\)|` + identRe + `)\s*=>`,
		ReturnBegin: true,
		End:         `\s*=>`,
		Contains: []*Mode{
			{
				ClassName: "params",
				Variants: []*Mode{
					{ClassName: "params", Begin: identRe},
					{ClassName: "params", Begin: `\(\s*\)`},
					{
						ClassName:    "params",
						Begin:        `\(`,
						End:          `\)`,
						ExcludeBegin: true,
						ExcludeEnd:   true,
						Keywords:     keywords,
						Contains:     paramsContains,
					},
				},
			},
		},
	}
	valueContainer := &Mode{
		Begin:    `(` + arcadeReStartersRe + `|\b(return)\b)\s*`,
		Keywords: Keywords{"keyword": "return"},
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			RegexpMode,
			functionValue,
		},
		HasRelevance: true,
		Relevance:    0,
	}

	return &Language{
		// Note: the reference also sets `illegal: /#(?!!)/`; RE2 has no negative
		// lookahead, so a stray `#` is not rejected here.
		Name: "ArcGIS Arcade",
		Mode: Mode{
			Keywords: keywords,
			Contains: []*Mode{
				AposStringMode,
				QuoteStringMode,
				templateString,
				CLineCommentMode,
				CBlockCommentMode,
				symbol,
				number,
				attrContainer,
				valueContainer,
				{
					ClassName:     "function",
					BeginKeywords: "function",
					End:           `\{`,
					ExcludeEnd:    true,
					Illegal:       `\[|%`,
					Contains: []*Mode{
						Variant(TitleMode, func(m *Mode) { m.Begin = identRe }),
						{
							ClassName:    "params",
							Begin:        `\(`,
							End:          `\)`,
							ExcludeBegin: true,
							ExcludeEnd:   true,
							Contains:     paramsContains,
						},
					},
				},
			},
		},
	}
}
