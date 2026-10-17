package highlight

func init() { RegisterLanguage("golo", Golo()) }

// Golo returns the Golo language definition (a port of languages/golo.js).
func Golo() *Language {
	return &Language{
		Name: "Golo",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "println readln print import module function local return let var " +
					"while for foreach times in case when match with break continue " +
					"augment augmentation each find filter reduce " +
					"if then else otherwise try catch finally raise throw orIfNull " +
					"DynamicObject|10 DynamicVariable struct Observable map set vector list array",
				"literal": "true false null",
			},
			Contains: []*Mode{
				HashCommentMode,
				QuoteStringMode,
				CNumberMode,
				{ClassName: "meta", Begin: `@[A-Za-z]+`},
			},
		},
	}
}
