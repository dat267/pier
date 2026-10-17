package highlight

func init() { RegisterLanguage("vbscript", VBScript()) }

// VBScript returns the VBScript language definition (a port of
// languages/vbscript.js). The built-in-call rule's begin is the alternation of
// every built-in function name followed by an open paren.
func VBScript() *Language {
	callBegin := `(lcase|month|vartype|instrrev|ubound|setlocale|getobject|rgb|getref|string|weekdayname|rnd|dateadd|monthname|now|day|minute|isarray|cbool|round|formatcurrency|conversions|csng|timevalue|second|year|space|abs|clng|timeserial|fixs|len|asc|isempty|maths|dateserial|atn|timer|isobject|filter|weekday|datevalue|ccur|isdate|instr|datediff|formatdatetime|replace|isnull|right|sgn|array|snumeric|log|cdbl|hex|chr|lbound|msgbox|ucase|getlocale|cos|cdate|cbyte|rtrim|join|hour|oct|typename|trim|strcomp|int|createobject|loadpicture|tan|formatnumber|mid|split||cint|sin|datepart|ltrim|sqr|time|derived|eval|date|formatpercent|exp|inputbox|left|ascw|chrw|regexp|cstr|err)\s*\(`
	call := &Mode{
		Begin:        callBegin,
		HasRelevance: true,
		Relevance:    0,
		Keywords: Keywords{"built_in": "lcase month vartype instrrev ubound setlocale getobject rgb getref string weekdayname rnd dateadd " +
			"monthname now day minute isarray cbool round formatcurrency conversions csng timevalue second year " +
			"space abs clng timeserial fixs len asc isempty maths dateserial atn timer isobject filter weekday " +
			"datevalue ccur isdate instr datediff formatdatetime replace isnull right sgn array snumeric log cdbl " +
			"hex chr lbound msgbox ucase getlocale cos cdate cbyte rtrim join hour oct typename trim strcomp int " +
			"createobject loadpicture tan formatnumber mid split cint sin datepart ltrim sqr time derived eval " +
			"date formatpercent exp inputbox left ascw chrw regexp cstr err"},
	}
	quote := Variant(QuoteStringMode, func(m *Mode) { m.Contains = []*Mode{{Begin: `""`}} })
	comment := CommentMode(`'`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0
	return &Language{
		Name:            "VBScript",
		Aliases:         []string{"vbs"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "call class const dim do loop erase execute executeglobal exit for each next function if then else on " +
					"error option explicit new private property let get public randomize redim rem select case set stop " +
					"sub while wend with end to elseif is or xor and not class_initialize class_terminate default " +
					"preserve in me byval byref step resume goto",
				"built_in": "server response request scriptengine scriptenginebuildversion scriptengineminorversion " +
					"scriptenginemajorversion",
				"literal": "true false null nothing empty",
			},
			Illegal: `//`,
			Contains: []*Mode{
				call,
				quote,
				comment,
				CNumberMode,
			},
		},
	}
}
