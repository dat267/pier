package highlight

func init() { RegisterLanguage("json", JSON()) }

// JSON returns the JSON language definition (a port of languages/json.js).
func JSON() *Language {
	const nonWhitespace = `[^\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`
	literals := Keywords{"literal": "true false null"}

	valueContainer := &Mode{
		End:            ",",
		EndsWithParent: true,
		ExcludeEnd:     true,
		Keywords:       literals,
	}
	withColon := Variant(valueContainer, func(m *Mode) { m.Begin = ":" })
	arrayValue := Variant(valueContainer, func(*Mode) {})
	object := &Mode{
		Begin:   `\{`,
		End:     `\}`,
		Illegal: nonWhitespace,
		Contains: []*Mode{
			{ClassName: "attr", Begin: `"`, End: `"`, Contains: []*Mode{BackslashEscape}, Illegal: `\n`},
			withColon,
			CLineCommentMode,
			CBlockCommentMode,
		},
	}
	array := &Mode{
		Begin:   `\[`,
		End:     `\]`,
		Illegal: nonWhitespace,
		Contains: []*Mode{
			arrayValue,
		},
	}

	// The value container's rule set is shared (the reference pushes into the
	// same TYPES array), so assign it after the container modes exist.
	types := []*Mode{QuoteStringMode, CNumberMode, object, array, CLineCommentMode, CBlockCommentMode}
	valueContainer.Contains = types
	withColon.Contains = types
	arrayValue.Contains = types

	return &Language{
		Name:     "JSON",
		Keywords: literals,
		Illegal:  nonWhitespace,
		Contains: types,
	}
}
