package highlight

func init() { RegisterLanguage("csp", CSP()) }

// CSP returns the CSP language definition (a port of languages/csp.js).
func CSP() *Language {
	return &Language{
		Name: "CSP",
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": `[a-zA-Z][a-zA-Z0-9_-]*`,
				"keyword": "base-uri child-src connect-src default-src font-src form-action " +
					"frame-ancestors frame-src img-src media-src object-src plugin-types " +
					"report-uri sandbox script-src style-src",
			},
			Contains: []*Mode{
				{ClassName: "string", Begin: `'`, End: `'`},
				{ClassName: "attribute", Begin: `^Content`, End: `:`, ExcludeEnd: true},
			},
		},
	}
}
