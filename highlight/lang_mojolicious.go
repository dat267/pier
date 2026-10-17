package highlight

func init() { RegisterLanguage("mojolicious", Mojolicious()) }

// Mojolicious returns the Mojolicious language definition (a port of
// languages/mojolicious.js). The root runs an XML sub-language; its own rules
// run Perl.
func Mojolicious() *Language {
	return &Language{
		Name: "Mojolicious",
		Mode: Mode{
			SubLanguage: []string{"xml"},
			Contains: []*Mode{
				{ClassName: "meta", Begin: `^__(END|DATA)__$`},
				{Begin: `^\s*%{1,2}={0,2}`, End: `$`, SubLanguage: []string{"perl"}},
				{
					Begin:        `<%{1,2}={0,2}`,
					End:          `={0,1}%>`,
					SubLanguage:  []string{"perl"},
					ExcludeBegin: true,
					ExcludeEnd:   true,
				},
			},
		},
	}
}
