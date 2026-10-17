package highlight

import "strings"

func init() { RegisterLanguage("routeros", RouterOS()) }

// RouterOS returns the Microtik RouterOS script language definition (a port of
// languages/routeros.js).
func RouterOS() *Language {
	const statements = "foreach do while for if from to step else on-error and or not in"
	const globalCommands = "global local beep delay put len typeof pick log time set find environment terminal error execute parse resolve toarray tobool toid toip toip6 tonum tostr totime"
	const commonCommands = "add remove enable disable set get print export edit find run debug error info warning"
	const literals = "true false yes no nothing nil null"
	const objects = "traffic-flow traffic-generator firewall scheduler aaa accounting address-list address align area bandwidth-server bfd bgp bridge client clock community config connection console customer default dhcp-client dhcp-server discovery dns e-mail ethernet filter firmware gps graphing group hardware health hotspot identity igmp-proxy incoming instance interface ip ipsec ipv6 irq l2tp-server lcd ldp logging mac-server mac-winbox mangle manual mirror mme mpls nat nd neighbor network note ntp ospf ospf-v3 ovpn-server page peer pim ping policy pool port ppp pppoe-client pptp-server prefix profile proposal proxy queue radius resource rip ripng route routing screen script security-profiles server service service-port settings shares smb sms sniffer snmp snooper socks sstp-server system tool tracking type upgrade upnp user-manager users user vlan secret vrrp watchdog web-access wireless pptp pppoe lan wan layer7-protocol lease simple raw"

	varMode := &Mode{ClassName: "variable"}
	varMode.Variants = []*Mode{
		Variant(varMode, func(m *Mode) { m.Begin = `\$[\w\d#@][\w\d_]*` }),
		Variant(varMode, func(m *Mode) { m.Begin = `\$\{(.*?)\}` }),
	}
	quoteString := &Mode{
		ClassName: "string",
		Begin:     `"`,
		End:       `"`,
		Contains: []*Mode{
			BackslashEscape,
			varMode,
			{ClassName: "variable", Begin: `\$\(`, End: `\)`, Contains: []*Mode{BackslashEscape}},
		},
	}
	aposString := &Mode{ClassName: "string", Begin: `'`, End: `'`}

	illegalSyntax := &Mode{Illegal: `.`}
	illegalSyntax.Variants = []*Mode{
		Variant(illegalSyntax, func(m *Mode) { m.Begin = `/\*`; m.End = `\*/` }),
		Variant(illegalSyntax, func(m *Mode) { m.Begin = `//`; m.End = `$` }),
		Variant(illegalSyntax, func(m *Mode) { m.Begin = `</`; m.End = `>` }),
	}

	attribute := &Mode{
		Begin:        `[\w-]+=([^\s{}[\]()>]+)`,
		HasRelevance: true,
		Relevance:    0,
		ReturnBegin:  true,
		Contains: []*Mode{
			{ClassName: "attribute", Begin: `[^=]+`},
			{
				Begin:          `=`,
				EndsWithParent: true,
				HasRelevance:   true,
				Relevance:      0,
				Contains: []*Mode{
					quoteString,
					aposString,
					varMode,
					{ClassName: "literal", Begin: `\b(` + strings.Join(strings.Split(literals, " "), "|") + `)\b`},
					{Begin: `("[^"]*"|[^\s{}[\]]+)`},
				},
			},
		},
	}

	common := &Mode{
		Begin:       `\b(` + strings.Join(strings.Split(commonCommands, " "), "|") + `)([\s[(\\]|])`,
		ReturnBegin: true,
		Contains:    []*Mode{{ClassName: "builtin-name", Begin: `\w+`}},
	}

	builtIn := &Mode{ClassName: "built_in"}
	builtIn.Variants = []*Mode{
		Variant(builtIn, func(m *Mode) {
			m.Begin = `(\.\./|/|\s)((` + strings.Join(strings.Split(objects, " "), "|") + `);?\s)+`
		}),
		Variant(builtIn, func(m *Mode) { m.Begin = `\.\.`; m.HasRelevance = true; m.Relevance = 0 }),
	}

	return &Language{
		Name:            "Microtik RouterOS script",
		Aliases:         []string{"mikrotik"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": ":?[\\w-]+",
				"literal":  "true false yes no nothing nil null",
				"keyword": "foreach do while for if from to step else on-error and or not in :foreach :do :while :for :if :from " +
					":to :step :else :on-error :and :or :not :in :global :local :beep :delay :put :len :typeof :pick :log " +
					":time :set :find :environment :terminal :error :execute :parse :resolve :toarray :tobool :toid :toip " +
					":toip6 :tonum :tostr :totime",
			},
			Contains: []*Mode{
				illegalSyntax,
				CommentMode(`^#`, `$`),
				quoteString,
				aposString,
				varMode,
				attribute,
				{ClassName: "number", Begin: `\*[0-9a-fA-F]+`},
				common,
				builtIn,
			},
		},
	}
}
