package highlight

func init() { RegisterLanguage("nix", Nix()) }

// Nix returns the Nix language definition (a port of languages/nix.js).
func Nix() *Language {
	keywords := Keywords{
		"keyword":  "rec with let in inherit assert if else then",
		"literal":  "true false or and null",
		"built_in": "import abort baseNameOf dirOf isNull builtins map removeAttrs throw toString derivation",
	}

	antiquote := &Mode{
		ClassName: "subst",
		Begin:     `\$\{`,
		End:       `\}`,
		Keywords:  keywords,
	}
	stringMode := &Mode{
		ClassName: "string",
		Contains:  []*Mode{antiquote},
		Variants: []*Mode{
			{ClassName: "string", Begin: "''", End: "''", Contains: []*Mode{antiquote}},
			{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{antiquote}},
		},
	}
	attrs := Zero(Mode{
		Begin:       `[a-zA-Z0-9_-]+(\s*=)`,
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "attr", Begin: `\S+`},
		},
	})

	expressions := []*Mode{
		NumberMode,
		HashCommentMode,
		CBlockCommentMode,
		stringMode,
		attrs,
	}
	// The antiquote rule set is shared with the string modes (the reference
	// assigns the same array), so set it after the container modes exist.
	antiquote.Contains = expressions

	return &Language{
		Name:    "Nix",
		Aliases: []string{"nixos"},
		Mode: Mode{
			Keywords: keywords,
			Contains: expressions,
		},
	}
}
