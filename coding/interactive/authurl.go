package interactive

import (
	"runtime"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Port of components/auth-url.ts: the sign-in URL block with a click hint and a
// copy hint. The host calls Copy when app.message.copy is pressed, because a
// long URL wraps and often cannot be selected or clicked as a whole (SSH, tmux).

// AuthUrlComponent is the sign-in URL block.
type AuthUrlComponent struct {
	*tui.Container
	url  string
	hint *tui.Text
	host tui.RenderRequester
	post func(func())
}

// NewAuthUrlComponent builds the block; post marshals the clipboard result onto
// the UI loop (nil applies it inline).
func NewAuthUrlComponent(host tui.RenderRequester, post func(func()), url string) *AuthUrlComponent {
	component := &AuthUrlComponent{Container: &tui.Container{}, url: url, host: host, post: post}
	component.AddChild(tui.NewText(ActiveTheme().Fg("accent", tui.Hyperlink(url, url)), 1, 0, nil))
	component.hint = tui.NewText("", 1, 0, nil)
	component.AddChild(component.hint)
	component.setHint(KeyHint("app.message.copy", "to copy"))
	return component
}

// URL returns the shown URL.
func (c *AuthUrlComponent) URL() string { return c.url }

// authURLCopier copies a sign-in URL; injectable so a test does not spawn a
// clipboard subprocess.
var authURLCopier = coding.CopyTextToClipboardAsync

// Copy copies the URL to the clipboard and reports the outcome in the hint.
func (c *AuthUrlComponent) Copy() {
	authURLCopier(c.url, func(err error) {
		apply := func() {
			if err != nil {
				c.setHint(ActiveTheme().Fg("error", err.Error()))
				return
			}
			c.setHint(ActiveTheme().Fg("success", "Copied URL to clipboard"))
		}
		if c.post != nil {
			c.post(apply)
			return
		}
		apply()
	})
}

func (c *AuthUrlComponent) setHint(suffix string) {
	theme := ActiveTheme()
	clickHint := "Ctrl+click to open"
	if runtime.GOOS == "darwin" {
		clickHint = "Cmd+click to open"
	}
	c.hint.SetText(theme.Fg("dim", tui.Hyperlink(clickHint, c.url)) + " " + theme.Fg("dim", "•") + " " + suffix)
	c.requestRender()
}

func (c *AuthUrlComponent) requestRender() {
	if c.host != nil {
		c.host.RequestRender(false)
	}
}
