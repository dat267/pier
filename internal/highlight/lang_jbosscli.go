package highlight

func init() { RegisterLanguage("jboss-cli", JBossCLI()) }

// JBossCLI returns the JBoss CLI language definition (a port of
// languages/jboss-cli.js).
func JBossCLI() *Language {
	param := &Mode{
		Begin:        `[\w-]+ *=`,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{{ClassName: "attr", Begin: `[\w-]+`}},
	}
	paramsBlock := &Mode{
		ClassName:    "params",
		Begin:        `\(`,
		End:          `\)`,
		Contains:     []*Mode{param},
		HasRelevance: true,
		Relevance:    0,
	}
	return &Language{
		Name:    "JBoss CLI",
		Aliases: []string{"wildfly-cli"},
		Keywords: Keywords{
			"$pattern": "[a-z-]+",
			"keyword": "alias batch cd clear command connect connection-factory connection-info data-source deploy " +
				"deployment-info deployment-overlay echo echo-dmr help history if jdbc-driver-info jms-queue|20 " +
				"jms-topic|20 ls patch pwd quit read-attribute read-operation reload rollout-plan run-batch set " +
				"shutdown try unalias undeploy unset version xa-data-source",
			"literal": "true false",
		},
		Contains: []*Mode{
			HashCommentMode,
			QuoteStringMode,
			{ClassName: "params", Begin: `--[\w\-=\/]+`},
			{ClassName: "function", Begin: `:[\w\-.]+`, HasRelevance: true, Relevance: 0},
			{ClassName: "string", Begin: `\B([\/.])[\w\-.\/=]+`},
			paramsBlock,
		},
	}
}
