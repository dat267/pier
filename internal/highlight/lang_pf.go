package highlight

func init() { RegisterLanguage("pf", PF()) }

// PF returns the Packet Filter config language definition (a port of
// languages/pf.js). The table rule's `<(?!\/)` lookahead becomes a guard.
func PF() *Language {
	table := &Mode{ClassName: "variable", Begin: `<`, End: `>`}
	table.beforeBegin = func(m *match, resp *Response) {
		if m.index+1 < len(m.input) && m.input[m.index+1] == '/' {
			resp.IgnoreMatch()
		}
	}
	return &Language{
		Name:    "Packet Filter config",
		Aliases: []string{"pf.conf"},
		Keywords: Keywords{
			"$pattern": "[a-z0-9_<>-]+",
			"built_in": "block match pass load anchor|5 antispoof|10 set table",
			"keyword": "in out log quick on rdomain inet inet6 proto from port os to route allow-opts divert-packet " +
				"divert-reply divert-to flags group icmp-type icmp6-type label once probability recieved-on rtable " +
				"prio queue tos tag tagged user keep fragment for os drop af-to|10 binat-to|10 nat-to|10 rdr-to|10 " +
				"bitmask least-stats random round-robin source-hash static-port dup-to reply-to route-to parent " +
				"bandwidth default min max qlimit block-policy debug fingerprints hostid limit loginterface " +
				"optimization reassemble ruleset-optimization basic none profile skip state-defaults state-policy " +
				"timeout const counters persist no modulate synproxy state|5 floating if-bound no-sync pflow|10 " +
				"sloppy source-track global rule max-src-nodes max-src-states max-src-conn max-src-conn-rate overload " +
				"flush scrub|5 max-mss min-ttl no-df|10 random-id",
			"literal": "all any no-route self urpf-failed egress|5 unknown",
		},
		Contains: []*Mode{
			HashCommentMode,
			NumberMode,
			QuoteStringMode,
			{ClassName: "variable", Begin: `\$[\w\d#@][\w\d_]*`},
			table,
		},
	}
}
