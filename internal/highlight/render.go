package highlight

import "strings"

// RenderHTML reproduces highlight.js's HTML renderer, so the port's output can
// be compared byte-for-byte with the reference (`hljs.highlight(...).value`).
func RenderHTML(nodes []*Node) string {
	var b strings.Builder
	writeHTML(&b, nodes, "hljs-")
	return b.String()
}

func writeHTML(b *strings.Builder, nodes []*Node, prefix string) {
	for _, n := range nodes {
		if n.Kind == "" && n.Children == nil {
			b.WriteString(escapeHTML(n.Text))
			continue
		}
		className := ""
		if n.Kind != "" {
			className = n.Kind
			if !n.Sublanguage {
				className = prefix + className
			}
			b.WriteString(`<span class="`)
			b.WriteString(className)
			b.WriteString(`">`)
		}
		writeHTML(b, n.Children, prefix)
		if n.Kind != "" {
			b.WriteString("</span>")
		}
	}
}

func escapeHTML(value string) string {
	if !strings.ContainsAny(value, `&<>"'`) {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#x27;")
		default:
			b.WriteByte(value[i])
		}
	}
	return b.String()
}
