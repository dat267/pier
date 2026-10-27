package highlight

func init() { RegisterLanguage("vbscript-html", VBScriptHTML()) }

// VBScriptHTML returns the VBScript-in-HTML language definition (a port of
// languages/vbscript-html.js).
func VBScriptHTML() *Language {
	return &Language{
		Name:        "VBScript in HTML",
		SubLanguage: []string{"xml"},
		Contains: []*Mode{
			{Begin: `<%`, End: `%>`, SubLanguage: []string{"vbscript"}},
		},
	}
}
