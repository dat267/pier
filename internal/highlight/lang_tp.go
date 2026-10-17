package highlight

func init() { RegisterLanguage("tp", TP()) }

// TP returns the TP language definition (a port of languages/tp.js).
func TP() *Language {
	tpID := &Mode{ClassName: "number", Begin: `[1-9][0-9]*`, HasRelevance: true, Relevance: 0}
	tpLabel := &Mode{ClassName: "symbol", Begin: `:[^\]]+`}

	tpData := &Mode{
		ClassName: "built_in",
		Begin: `(AR|P|PAYLOAD|PR|R|SR|RSR|LBL|VR|UALM|MESSAGE|UTOOL|UFRAME|TIMER|` +
			`TIMER_OVERFLOW|JOINT_MAX_SPEED|RESUME_PROG|DIAG_REC)\[`,
		End:      `\]`,
		Contains: []*Mode{Self, tpID, tpLabel},
	}
	tpIO := &Mode{
		ClassName: "built_in",
		Begin:     `(AI|AO|DI|DO|F|RI|RO|UI|UO|GI|GO|SI|SO)\[`,
		End:       `\]`,
		Contains:  []*Mode{Self, tpID, QuoteStringMode, tpLabel},
	}

	return &Language{
		Name: "TP",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "ABORT ACC ADJUST AND AP_LD BREAK CALL CNT COL CONDITION CONFIG DA DB " +
					"DIV DETECT ELSE END ENDFOR ERR_NUM ERROR_PROG FINE FOR GP GUARD INC " +
					"IF JMP LINEAR_MAX_SPEED LOCK MOD MONITOR OFFSET Offset OR OVERRIDE " +
					"PAUSE PREG PTH RT_LD RUN SELECT SKIP Skip TA TB TO TOOL_OFFSET " +
					"Tool_Offset UF UT UFRAME_NUM UTOOL_NUM UNLOCK WAIT X Y Z W P R STRLEN " +
					"SUBSTR FINDSTR VOFFSET PROG ATTR MN POS",
				"literal": "ON OFF max_speed LPOS JPOS ENABLE DISABLE START STOP RESET",
			},
			Contains: []*Mode{
				tpData,
				tpIO,
				{ClassName: "keyword", Begin: `/(PROG|ATTR|MN|POS|END)\b`},
				{ClassName: "keyword", Begin: `(CALL|RUN|POINT_LOGIC|LBL)\b`},
				{ClassName: "keyword", Begin: `\b(ACC|CNT|Skip|Offset|PSPD|RT_LD|AP_LD|Tool_Offset)`},
				{
					ClassName:    "number",
					Begin:        `\d+(sec|msec|mm/sec|cm/min|inch/min|deg/sec|mm|in|cm)?\b`,
					HasRelevance: true,
					Relevance:    0,
				},
				CommentMode(`//`, `[;$]`),
				CommentMode(`!`, `[;$]`),
				CommentMode(`--eg:`, `$`),
				QuoteStringMode,
				{ClassName: "string", Begin: `'`, End: `'`},
				CNumberMode,
				{ClassName: "variable", Begin: `\$[A-Za-z0-9_]+`},
			},
		},
	}
}
