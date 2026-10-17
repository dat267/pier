package highlight

func init() { RegisterLanguage("nim", Nim()) }

// Nim returns the Nim language definition (a port of languages/nim.js).
func Nim() *Language {
	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `\b(0[xX][0-9a-fA-F][_0-9a-fA-F]*)('?[iIuU](8|16|32|64))?` }),
		Variant(number, func(m *Mode) { m.Begin = `\b(0o[0-7][_0-7]*)('?[iIuUfF](8|16|32|64))?` }),
		Variant(number, func(m *Mode) { m.Begin = `\b(0(b|B)[01][_01]*)('?[iIuUfF](8|16|32|64))?` }),
		Variant(number, func(m *Mode) { m.Begin = `\b(\d[_\d]*)('?[iIuUfF](8|16|32|64))?` }),
	}
	return &Language{
		Name: "Nim",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "addr and as asm bind block break case cast const continue converter discard distinct div do elif " +
					"else end enum except export finally for from func generic if import in include interface is isnot " +
					"iterator let macro method mixin mod nil not notin object of or out proc ptr raise ref return shl shr " +
					"static template try tuple type using var when while with without xor yield",
				"literal": "shared guarded stdin stdout stderr result true false",
				"built_in": "int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float float32 float64 bool char string " +
					"cstring pointer expr stmt void auto any range array openarray varargs seq set clong culong cchar " +
					"cschar cshort cint csize clonglong cfloat cdouble clongdouble cuchar cushort cuint culonglong " +
					"cstringarray semistatic",
			},
			Contains: []*Mode{
				{ClassName: "meta", Begin: `\{\.`, End: `\.\}`, HasRelevance: true, Relevance: 10},
				{ClassName: "string", Begin: `[a-zA-Z]\w*"`, End: `"`, Contains: []*Mode{{Begin: `""`}}},
				{ClassName: "string", Begin: `([a-zA-Z]\w*)?"""`, End: `"""`},
				QuoteStringMode,
				{ClassName: "type", Begin: `\b[A-Z]\w+\b`, HasRelevance: true, Relevance: 0},
				number,
				HashCommentMode,
			},
		},
	}
}
