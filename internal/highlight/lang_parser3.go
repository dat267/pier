package highlight

func init() { RegisterLanguage("parser3", Parser3()) }

// Parser3 returns the Parser3 language definition (a port of
// languages/parser3.js). The root runs an XML sub-language over everything its
// own rules do not match.
func Parser3() *Language {
	curlySubcomment := CommentMode(`\{`, `\}`)
	curlySubcomment.Contains = append(curlySubcomment.Contains, Self)

	remComment := CommentMode(`\^rem\{`, `\}`)
	remComment.HasRelevance = true
	remComment.Relevance = 10
	remComment.Contains = append(remComment.Contains, curlySubcomment)

	return &Language{
		Name: "Parser3",
		Mode: Mode{
			SubLanguage:  []string{"xml"},
			HasRelevance: true,
			Relevance:    0,
			Contains: []*Mode{
				CommentMode(`^#`, `$`),
				remComment,
				{ClassName: "meta", Begin: `^@(?:BASE|USE|CLASS|OPTIONS)$`, HasRelevance: true, Relevance: 10},
				{ClassName: "title", Begin: `@[\w\-]+\[[\w^;\-]*\](?:\[[\w^;\-]*\])?(?:.*)$`},
				{ClassName: "variable", Begin: `\$\{?[\w\-.:]+\}?`},
				{ClassName: "keyword", Begin: `\^[\w\-.:]+`},
				{ClassName: "number", Begin: `\^#[0-9a-fA-F]+`},
				CNumberMode,
			},
		},
	}
}
