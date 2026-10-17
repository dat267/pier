package highlight

func init() { RegisterLanguage("twig", Twig()) }

// Twig returns the Twig language definition (a port of languages/twig.js).
func Twig() *Language {
	params := &Mode{ClassName: "params", Begin: `\(`, End: `\)`}

	functions := &Mode{
		BeginKeywords: "attribute block constant cycle date dump include max min parent random range source " +
			"template_from_string",
		Keywords: Keywords{"name": "attribute block constant cycle date dump include max min parent random range source " +
			"template_from_string"},
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{params},
	}
	filter := &Mode{
		Begin: `\|[A-Za-z_]+:?`,
		Keywords: Keywords{"keyword": "abs batch capitalize column convert_encoding date date_modify default escape filter first " +
			"format inky_to_html inline_css join json_encode keys last length lower map markdown merge " +
			"nl2br number_format raw reduce replace reverse round slice sort spaceless split striptags " +
			"title trim upper url_encode"},
		Contains: []*Mode{functions},
	}

	nameMode := &Mode{
		ClassName: "name",
		Begin:     `\w+`,
		Keywords: Keywords{"keyword": "apply autoescape block deprecated do embed extends filter flush for from if import include " +
			"macro sandbox set use verbatim with endapply endautoescape endblock enddeprecated enddo " +
			"endembed endextends endfilter endflush endfor endfrom endif endimport endinclude endmacro " +
			"endsandbox endset enduse endverbatim endwith"},
		Starts: &Mode{
			EndsWithParent: true,
			Contains:       []*Mode{filter, functions},
			HasRelevance:   true,
			Relevance:      0,
		},
	}

	return &Language{
		Name:            "Twig",
		Aliases:         []string{"craftcms"},
		CaseInsensitive: true,
		Mode: Mode{
			SubLanguage: []string{"xml"},
			Contains: []*Mode{
				CommentMode(`\{#`, `#\}`),
				{
					ClassName: "template-tag",
					Begin:     `\{%`,
					End:       `%\}`,
					Contains:  []*Mode{nameMode},
				},
				{
					ClassName: "template-variable",
					Begin:     `\{\{`,
					End:       `\}\}`,
					Contains:  []*Mode{Self, filter, functions},
				},
			},
		},
	}
}
