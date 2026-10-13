package interactive

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v3"
	"github.com/alecthomas/chroma/v3/lexers"
)

var (
	chromaLanguagesOnce sync.Once
	chromaLanguages     map[string]struct{}
)

func knownChromaLexer(language string) chroma.Lexer {
	chromaLanguagesOnce.Do(func() {
		chromaLanguages = make(map[string]struct{})
		for _, name := range lexers.Names(true) {
			chromaLanguages[strings.ToLower(name)] = struct{}{}
		}
	})
	if _, ok := chromaLanguages[strings.ToLower(language)]; !ok {
		return nil
	}
	return lexers.Get(language)
}

func syntaxTokenColor(token chroma.TokenType) string {
	switch {
	case token.InCategory(chroma.Keyword), token == chroma.NameKeyword:
		return "syntaxKeyword"
	case token == chroma.NameFunction || token == chroma.NameFunctionMagic:
		return "syntaxFunction"
	case token == chroma.NameClass, token == chroma.NameBuiltin,
		token == chroma.NameBuiltinPseudo, token == chroma.KeywordType:
		return "syntaxType"
	case token.InSubCategory(chroma.LiteralNumber):
		return "syntaxNumber"
	case token.InSubCategory(chroma.LiteralString), token.InCategory(chroma.Comment):
		if token.InCategory(chroma.Comment) {
			return "syntaxComment"
		}
		return "syntaxString"
	case token.InCategory(chroma.Operator), token == chroma.NameOperator:
		return "syntaxOperator"
	case token.InCategory(chroma.Punctuation), token == chroma.NameTag:
		return "syntaxPunctuation"
	case token.InCategory(chroma.NameVariable), token == chroma.NameAttribute,
		token == chroma.NameProperty, token == chroma.NameConstant:
		return "syntaxVariable"
	case token.InCategory(chroma.Generic):
		if token == chroma.GenericInserted {
			return "toolDiffAdded"
		}
		if token == chroma.GenericDeleted {
			return "toolDiffRemoved"
		}
	}
	return "mdCodeBlock"
}

func highlightCodeWithChroma(code string, language string, theme *Theme) []string {
	lexer := knownChromaLexer(language)
	if lexer == nil {
		return highlightPlainCode(code, theme)
	}
	tokens, err := chroma.Tokenise(chroma.Coalesce(lexer), &chroma.TokeniseOptions{State: "root", EnsureLF: true}, code)
	if err != nil {
		return highlightPlainCode(code, theme)
	}
	var output strings.Builder
	for _, token := range tokens {
		color := syntaxTokenColor(token.Type)
		output.WriteString(theme.Fg(color, token.Value))
	}
	return strings.Split(output.String(), "\n")
}

func highlightPlainCode(code string, theme *Theme) []string {
	lines := strings.Split(code, "\n")
	for index, line := range lines {
		lines[index] = theme.Fg("mdCodeBlock", line)
	}
	return lines
}
