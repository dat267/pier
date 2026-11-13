package interactive

import (
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// webLink renders text as an ST-terminated terminal hyperlink to url when url
// is a web URL, else as plain text (upstream's webLink helper). The framing is
// the renderer's own (tui.Hyperlink), so a component holds no escape string of
// its own that untrusted text could end early, and a target that is not a web
// URL is never linked.
func webLink(text, url string) string {
	normalized, ok := coding.WebURL(url)
	if !ok {
		return text
	}
	return tui.Hyperlink(text, normalized)
}
