package highlight

func init() { RegisterLanguage("openscad", OpenSCAD()) }

// OpenSCAD returns the OpenSCAD language definition (a port of
// languages/openscad.js).
func OpenSCAD() *Language {
	specialVars := &Mode{ClassName: "keyword", Begin: `\$(f[asn]|t|vp[rtd]|children)`}
	literals := &Mode{ClassName: "literal", Begin: `false|true|PI|undef`}
	numbers := &Mode{ClassName: "number", Begin: `\b\d+(\.\d+)?(e-?\d+)?`, HasRelevance: true, Relevance: 0}
	str := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	prepro := &Mode{
		ClassName: "meta",
		Keywords:  Keywords{"meta-keyword": "include use"},
		Begin:     `include|use <`,
		End:       `>`,
	}
	params := &Mode{
		ClassName: "params",
		Begin:     `\(`,
		End:       `\)`,
		Contains:  []*Mode{Self, numbers, str, specialVars, literals},
	}
	modifiers := &Mode{Begin: `[*!#%]`, HasRelevance: true, Relevance: 0}
	functions := &Mode{
		ClassName:     "function",
		BeginKeywords: "module function",
		End:           `=|\{`,
		Contains:      []*Mode{params, UnderscoreTitleMode},
	}
	return &Language{
		Name:    "OpenSCAD",
		Aliases: []string{"scad"},
		Keywords: Keywords{
			"keyword": "function module include use for intersection_for if else \\%",
			"literal": "false true PI undef",
			"built_in": "circle square polygon text sphere cube cylinder polyhedron translate rotate scale resize mirror " +
				"multmatrix color offset hull minkowski union difference intersection abs sign sin cos tan acos asin " +
				"atan atan2 floor round ceil ln log pow sqrt exp rands min max concat lookup str chr search version " +
				"version_num norm cross parent_module echo import import_dxf dxf_linear_extrude linear_extrude " +
				"rotate_extrude surface projection render children dxf_cross dxf_dim let assign",
		},
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			numbers,
			prepro,
			str,
			specialVars,
			modifiers,
			functions,
		},
	}
}
