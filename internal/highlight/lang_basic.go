package highlight

func init() { RegisterLanguage("basic", BASIC()) }

// BASIC returns the BASIC language definition (a port of languages/basic.js).
func BASIC() *Language {
	rem := CommentMode(`REM`, `$`)
	rem.HasRelevance = true
	rem.Relevance = 10
	apos := CommentMode(`'`, `$`)
	apos.HasRelevance = true
	apos.Relevance = 0
	return &Language{
		Name:            "BASIC",
		CaseInsensitive: true,
		Illegal:         `^\.`,
		Keywords: Keywords{
			"$pattern": "[a-zA-Z][a-zA-Z0-9_$%!#]*",
			"keyword": "ABS ASC AND ATN AUTO|0 BEEP BLOAD|10 BSAVE|10 CALL CALLS CDBL CHAIN CHDIR CHR$|10 CINT CIRCLE CLEAR " +
				"CLOSE CLS COLOR COM COMMON CONT COS CSNG CSRLIN CVD CVI CVS DATA DATE$ DEFDBL DEFINT DEFSNG DEFSTR " +
				"DEF|0 SEG USR DELETE DIM DRAW EDIT END ENVIRON ENVIRON$ EOF EQV ERASE ERDEV ERDEV$ ERL ERR ERROR EXP " +
				"FIELD FILES FIX FOR|0 FRE GET GOSUB|10 GOTO HEX$ IF THEN ELSE|0 INKEY$ INP INPUT INPUT# INPUT$ INSTR " +
				"IMP INT IOCTL IOCTL$ KEY ON OFF LIST KILL LEFT$ LEN LET LINE LLIST LOAD LOC LOCATE LOF LOG LPRINT " +
				"USING LSET MERGE MID$ MKDIR MKD$ MKI$ MKS$ MOD NAME NEW NEXT NOISE NOT OCT$ ON OR PEN PLAY STRIG " +
				"OPEN OPTION BASE OUT PAINT PALETTE PCOPY PEEK PMAP POINT POKE POS PRINT PRINT] PSET PRESET PUT " +
				"RANDOMIZE READ REM RENUM RESET|0 RESTORE RESUME RETURN|0 RIGHT$ RMDIR RND RSET RUN SAVE SCREEN SGN " +
				"SHELL SIN SOUND SPACE$ SPC SQR STEP STICK STOP STR$ STRING$ SWAP SYSTEM TAB TAN TIME$ TIMER TROFF " +
				"TRON TO USR VAL VARPTR VARPTR$ VIEW WAIT WHILE WEND WIDTH WINDOW WRITE XOR",
		},
		Contains: []*Mode{
			QuoteStringMode,
			rem,
			apos,
			{ClassName: "symbol", Begin: `^[0-9]+ `, HasRelevance: true, Relevance: 10},
			{ClassName: "number", Begin: `\b\d+(\.\d+)?([edED]\d+)?[#\!]?`, HasRelevance: true, Relevance: 0},
			{ClassName: "number", Begin: `(&[hH][0-9a-fA-F]{1,4})`},
			{ClassName: "number", Begin: `(&[oO][0-7]{1,6})`},
		},
	}
}
