package highlight

func init() { RegisterLanguage("purebasic", PureBasic()) }

// PureBasic returns the PureBASIC language definition (a port of
// languages/purebasic.js).
func PureBasic() *Language {
	comment := CommentMode(`;`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0

	function := &Mode{
		ClassName:   "function",
		Begin:       `\b(Procedure|Declare)(C|CDLL|DLL)?\b`,
		End:         `\(`,
		ExcludeEnd:  true,
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "keyword", Begin: `(Procedure|Declare)(C|CDLL|DLL)?`, ExcludeEnd: true},
			{ClassName: "type", Begin: `\.\w*`},
			UnderscoreTitleMode,
		},
	}

	return &Language{
		Name:    "PureBASIC",
		Aliases: []string{"pb", "pbi"},
		Keywords: Keywords{
			"keyword": "Align And Array As Break CallDebugger Case CompilerCase CompilerDefault CompilerElse CompilerElseIf " +
				"CompilerEndIf CompilerEndSelect CompilerError CompilerIf CompilerSelect CompilerWarning Continue " +
				"Data DataSection Debug DebugLevel Declare DeclareC DeclareCDLL DeclareDLL DeclareModule Default " +
				"Define Dim DisableASM DisableDebugger DisableExplicit Else ElseIf EnableASM EnableDebugger " +
				"EnableExplicit End EndDataSection EndDeclareModule EndEnumeration EndIf EndImport EndInterface " +
				"EndMacro EndModule EndProcedure EndSelect EndStructure EndStructureUnion EndWith Enumeration " +
				"EnumerationBinary Extends FakeReturn For ForEach ForEver Global Gosub Goto If Import ImportC " +
				"IncludeBinary IncludeFile IncludePath Interface List Macro MacroExpandedCount Map Module NewList " +
				"NewMap Next Not Or Procedure ProcedureC ProcedureCDLL ProcedureDLL ProcedureReturn Protected " +
				"Prototype PrototypeC ReDim Read Repeat Restore Return Runtime Select Shared Static Step Structure " +
				"StructureUnion Swap Threaded To UndefineMacro Until Until  UnuseModule UseModule Wend While With " +
				"XIncludeFile XOr",
		},
		Contains: []*Mode{
			comment,
			function,
			{ClassName: "string", Begin: `(~)?"`, End: `"`, Illegal: `\n`},
			{ClassName: "symbol", Begin: `#[a-zA-Z_]\w*\$?`},
		},
	}
}
