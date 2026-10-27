package oauth

import "strings"

// OrderedForm is a URLSearchParams-compatible name/value list: entries keep
// insertion order, `Set` replaces a name in place, and `Encode` serializes with
// the application/x-www-form-urlencoded rules the reference's `URLSearchParams`
// uses. Go's `url.Values` sorts keys and escapes `*`/`~` differently, which
// changes the request bytes the authorization server sees.
type OrderedForm struct {
	entries []formEntry
}

type formEntry struct {
	name  string
	value string
}

// NewOrderedForm parses a raw query string (without the leading `?`), keeping
// the reference parser's semantics: empty segments are skipped, a segment
// without `=` has an empty value, and `+`/percent escapes are decoded.
func NewOrderedForm(rawQuery string) *OrderedForm {
	form := &OrderedForm{}
	for part := range strings.SplitSeq(rawQuery, "&") {
		if part == "" {
			continue
		}
		name, value := part, ""
		if before, after, ok := strings.Cut(part, "="); ok {
			name, value = before, after
		}
		form.entries = append(form.entries, formEntry{
			name:  decodeFormComponent(name),
			value: decodeFormComponent(value),
		})
	}
	return form
}

// Set replaces the first entry named name in place and drops any later ones,
// or appends the entry when the name is absent (URLSearchParams.set).
func (f *OrderedForm) Set(name, value string) {
	replaced := false
	kept := f.entries[:0]
	for _, entry := range f.entries {
		if entry.name != name {
			kept = append(kept, entry)
			continue
		}
		if !replaced {
			kept = append(kept, formEntry{name: name, value: value})
			replaced = true
		}
	}
	f.entries = kept
	if !replaced {
		f.entries = append(f.entries, formEntry{name: name, value: value})
	}
}

// Append adds an entry without replacing an existing one.
func (f *OrderedForm) Append(name, value string) {
	f.entries = append(f.entries, formEntry{name: name, value: value})
}

// Get returns the first value for name.
func (f *OrderedForm) Get(name string) (string, bool) {
	for _, entry := range f.entries {
		if entry.name == name {
			return entry.value, true
		}
	}
	return "", false
}

// Has reports whether the form carries name.
func (f *OrderedForm) Has(name string) bool {
	_, ok := f.Get(name)
	return ok
}

// Encode serializes the form in insertion order with the URLSearchParams
// escaping: alphanumerics and `*-._` stay raw, a space becomes `+`, every other
// byte is percent-encoded with uppercase hex.
func (f *OrderedForm) Encode() string {
	var builder strings.Builder
	for index, entry := range f.entries {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(encodeFormComponent(entry.name))
		builder.WriteByte('=')
		builder.WriteString(encodeFormComponent(entry.value))
	}
	return builder.String()
}

func encodeFormComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for _, char := range []byte(value) {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9',
			char == '*', char == '-', char == '.', char == '_':
			builder.WriteByte(char)
		case char == ' ':
			builder.WriteByte('+')
		default:
			builder.WriteByte('%')
			builder.WriteByte(hex[char>>4])
			builder.WriteByte(hex[char&0x0f])
		}
	}
	return builder.String()
}

// decodeFormComponent decodes a form component: `+` is a space and valid
// percent escapes are decoded; other bytes, including a malformed escape, stay
// as they are.
func decodeFormComponent(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char == '+':
			builder.WriteByte(' ')
		case char == '%' && index+2 < len(value) && isHexDigit(value[index+1]) && isHexDigit(value[index+2]):
			builder.WriteByte(unhexDigit(value[index+1])<<4 | unhexDigit(value[index+2]))
			index += 2
		default:
			builder.WriteByte(char)
		}
	}
	return builder.String()
}

func isHexDigit(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}

func unhexDigit(char byte) byte {
	switch {
	case char >= '0' && char <= '9':
		return char - '0'
	case char >= 'a' && char <= 'f':
		return char - 'a' + 10
	default:
		return char - 'A' + 10
	}
}
