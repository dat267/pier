package highlight

func init() { RegisterLanguage("c-like", CLike()) }

// CLike returns the deprecated C-like definition (a port of languages/c-like.js):
// the C++ definition with autodetection disabled and no aliases, because `c` and
// `cpp` are registered and reclaim their aliases.
func CLike() *Language {
	lang := CPP()
	lang.DisableAutodetect = true
	lang.Aliases = nil
	return lang
}
