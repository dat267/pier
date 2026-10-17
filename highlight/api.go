package highlight

import (
	"fmt"
	"sort"
	"sync"
)

// compiledLanguage is a language compiled for highlighting and cached.
type compiledLanguage struct {
	name              string
	aliases           []string
	caseInsensitive   bool
	flags             string
	classNameAliases  map[string]string
	disableAutodetect bool
	root              *compiledMode
}

var (
	registryMu sync.RWMutex
	registry   = map[string]*Language{}

	compiledMu        sync.RWMutex
	compiledLanguages = map[string]*compiledLanguage{}
)

// RegisterLanguage registers a language definition under name and its aliases.
func RegisterLanguage(name string, language *Language) {
	language.Name = name
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = language
	for _, alias := range language.Aliases {
		registry[alias] = language
	}
}

// supportsLanguage reports whether a language (or alias) is registered.
func supportsLanguage(name string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[name]
	return ok
}

func getCompiledLanguage(name string) (*compiledLanguage, error) {
	compiledMu.RLock()
	if lang, ok := compiledLanguages[name]; ok {
		compiledMu.RUnlock()
		return lang, nil
	}
	compiledMu.RUnlock()

	compiledMu.Lock()
	defer compiledMu.Unlock()
	if lang, ok := compiledLanguages[name]; ok {
		return lang, nil
	}
	registryMu.RLock()
	language, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("highlight: unknown language %q", name)
	}
	c := &compiler{language: language, compiled: map[*Mode]*compiledMode{}}
	root, err := c.compileLanguageFor(language)
	if err != nil {
		return nil, fmt.Errorf("highlight: compile %q: %w", name, err)
	}
	flags := "m"
	if language.CaseInsensitive {
		flags += "i"
	}
	lang := &compiledLanguage{
		name:              language.Name,
		aliases:           language.Aliases,
		caseInsensitive:   language.CaseInsensitive,
		flags:             flags,
		classNameAliases:  language.ClassNameAliases,
		disableAutodetect: language.DisableAutodetect,
		root:              root,
	}
	compiledLanguages[name] = lang
	if language.Name != name {
		compiledLanguages[language.Name] = lang
	}
	for _, alias := range language.Aliases {
		compiledLanguages[alias] = lang
	}
	return lang, nil
}

func (c *compiler) compileLanguageFor(language *Language) (*compiledMode, error) {
	for _, m := range language.Contains {
		if m == Self {
			return nil, fmt.Errorf("contains `self` at the top level")
		}
	}
	root := cloneTree(&language.Mode, map[*Mode]*Mode{})
	return c.compileMode(root, nil)
}

// SupportsLanguage reports whether name is a registered language or alias.
func SupportsLanguage(name string) bool {
	return supportsLanguage(name)
}

// Highlight highlights code with the named language and returns the scope tree.
// An illegal lexeme aborts the highlight and returns the input as plain text,
// matching the reference's default (ignoreIllegals false).
func Highlight(code, language string) (*Result, error) {
	return HighlightWith(code, language, false)
}

// HighlightWith is Highlight with the reference's ignoreIllegals option.
func HighlightWith(code, language string, ignoreIllegals bool) (*Result, error) {
	lang, err := getCompiledLanguage(language)
	if err != nil {
		return nil, err
	}
	return highlightWith(lang, code, ignoreIllegals), nil
}

// Languages returns the sorted names of every registered language.
func Languages() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
