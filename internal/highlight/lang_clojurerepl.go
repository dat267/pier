package highlight

func init() { RegisterLanguage("clojure-repl", ClojureRepl()) }

// ClojureRepl returns the Clojure REPL language definition (a port of
// languages/clojure-repl.js).
func ClojureRepl() *Language {
	meta := &Mode{
		ClassName: "meta",
		Begin:     `^([\w.-]+|\s*#_)?=>`,
		Starts:    &Mode{End: `$`, SubLanguage: []string{"clojure"}},
	}
	return &Language{
		Name:     "Clojure REPL",
		Contains: []*Mode{meta},
	}
}
