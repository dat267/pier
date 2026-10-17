package highlight

func init() { RegisterLanguage("plaintext", Plaintext()) }

// Plaintext returns the plain-text language definition (a port of
// languages/plaintext.js). It disables autodetection and defines no rules, so
// the input is emitted as one plain text node.
func Plaintext() *Language {
	return &Language{
		Name:              "Plain text",
		Aliases:           []string{"text", "txt"},
		DisableAutodetect: true,
	}
}
