package highlight

func init() { RegisterLanguage("php-template", PHPTemplate()) }

// PHPTemplate returns the PHP template language definition (a port of
// languages/php-template.js). The root runs an XML sub-language; `<?php ... ?>`
// runs PHP. The skip modes hide the closing tag inside comments and strings.
func PHPTemplate() *Language {
	plainString := func(m *Mode) {
		m.Illegal = ""
		m.ClassName = ""
		m.Contains = nil
		m.Skip = true
	}
	return &Language{
		Name: "PHP template",
		Mode: Mode{
			SubLanguage: []string{"xml"},
			Contains: []*Mode{
				{
					Begin:       `<\?(php|=)?`,
					End:         `\?>`,
					SubLanguage: []string{"php"},
					Contains: []*Mode{
						{Begin: `/\*`, End: `\*/`, Skip: true},
						{Begin: `b"`, End: `"`, Skip: true},
						{Begin: `b'`, End: `'`, Skip: true},
						Variant(AposStringMode, plainString),
						Variant(QuoteStringMode, plainString),
					},
				},
			},
		},
	}
}
