package highlight

func init() { RegisterLanguage("dockerfile", Dockerfile()) }

// Dockerfile returns the Dockerfile language definition (a port of
// languages/dockerfile.js). A RUN/CMD/... rule starts a bash sub-language that
// ends at a line not ending in a backslash.
func Dockerfile() *Language {
	return &Language{
		Name:            "Dockerfile",
		Aliases:         []string{"docker"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "from maintainer expose env arg user onbuild stopsignal",
			},
			Contains: []*Mode{
				HashCommentMode,
				AposStringMode,
				QuoteStringMode,
				NumberMode,
				{
					BeginKeywords: "run cmd entrypoint volume add copy workdir label healthcheck shell",
					Starts: &Mode{
						End:         `[^\\]$`,
						SubLanguage: []string{"bash"},
					},
				},
			},
			Illegal: "</",
		},
	}
}
