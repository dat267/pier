package highlight

func init() { RegisterLanguage("clean", Clean()) }

// Clean returns the Clean language definition (a port of languages/clean.js).
func Clean() *Language {
	return &Language{
		Name:    "Clean",
		Aliases: []string{"icl", "dcl"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "if let in with where case of class instance otherwise " +
					"implementation definition system module from import qualified as " +
					"special code inline foreign export ccall stdcall generic derive " +
					"infix infixl infixr",
				"built_in": "Int Real Char Bool",
				"literal":  "True False",
			},
			Contains: []*Mode{
				CLineCommentMode,
				CBlockCommentMode,
				AposStringMode,
				QuoteStringMode,
				CNumberMode,
				// relevance booster
				{Begin: `->|<-[|:]?|#!?|>>=|\{\||\|\}|:==|=:|<>`},
			},
		},
	}
}
