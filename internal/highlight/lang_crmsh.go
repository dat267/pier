package highlight

import "strings"

func init() { RegisterLanguage("crmsh", Crmsh()) }

// Crmsh returns the crmsh language definition (a port of languages/crmsh.js).
func Crmsh() *Language {
	const resources = "primitive rsc_template"
	const commands = "group clone ms master location colocation order fencing_topology " +
		"rsc_ticket acl_target acl_group user role tag xml"
	const propertySets = "property rsc_defaults op_defaults"

	return &Language{
		Name:            "crmsh",
		Aliases:         []string{"crm", "pcmk"},
		CaseInsensitive: true,
		Keywords: Keywords{
			"keyword": "params meta operations op rule attributes utilization read write deny defined not_defined in_range " +
				"date spec in ref reference attribute type xpath version and or lt gt tag lte gte eq ne \\ number " +
				"string",
			"literal": "Master Started Slave Stopped start promote demote stop monitor true false",
		},
		Contains: []*Mode{
			HashCommentMode,
			{
				BeginKeywords: "node",
				Starts: &Mode{
					End: `\s*([\w_-]+:)?`,
					Starts: &Mode{
						ClassName: "title",
						End:       `\s*[\$\w_][\w_-]*`,
					},
				},
			},
			{
				BeginKeywords: resources,
				Starts: &Mode{
					ClassName: "title",
					End:       `\s*[\$\w_][\w_-]*`,
					Starts:    &Mode{End: `\s*@?[\w_][\w_\.:-]*`},
				},
			},
			{
				Begin:    `\b(` + strings.Join(strings.Split(commands, " "), "|") + `)\s+`,
				Keywords: Keywords{"keyword": commands},
				Starts:   &Mode{ClassName: "title", End: `[\$\w_][\w_-]*`},
			},
			{
				BeginKeywords: propertySets,
				Starts:        &Mode{ClassName: "title", End: `\s*([\w_-]+:)?`},
			},
			QuoteStringMode,
			{ClassName: "meta", Begin: `(ocf|systemd|service|lsb):[\w_:-]+`, HasRelevance: true, Relevance: 0},
			{ClassName: "number", Begin: `\b\d+(\.\d+)?(ms|s|h|m)?`, HasRelevance: true, Relevance: 0},
			{ClassName: "literal", Begin: `[-]?(infinity|inf)`, HasRelevance: true, Relevance: 0},
			{ClassName: "attr", Begin: `([A-Za-z$_#][\w_-]+)=`, HasRelevance: true, Relevance: 0},
			{ClassName: "tag", Begin: `</?`, End: `/?>`, HasRelevance: true, Relevance: 0},
		},
	}
}
