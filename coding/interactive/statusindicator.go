package interactive

import (
	"strconv"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// Port of src/modes/interactive/components/{status-indicator,
// branch-summary-message, compaction-summary-message}.ts.

// StatusIndicatorKind classifies the status indicator.
type StatusIndicatorKind string

const (
	StatusWorking       StatusIndicatorKind = "working"
	StatusRetry         StatusIndicatorKind = "retry"
	StatusCompaction    StatusIndicatorKind = "compaction"
	StatusBranchSummary StatusIndicatorKind = "branchSummary"
)

// StatusIndicator is a loader with a kind and border-aware rendering.
type StatusIndicator struct {
	*tui.Loader

	Kind StatusIndicatorKind
}

// IndicatorKind returns the indicator kind (StatusIndicatorLike).
func (s *StatusIndicator) IndicatorKind() StatusIndicatorKind { return s.Kind }

// StatusIndicatorLike is the indicator surface the UI state manages.
type StatusIndicatorLike interface {
	tui.Component
	IndicatorKind() StatusIndicatorKind
	Dispose()
}

// NewStatusIndicator creates the indicator.
func NewStatusIndicator(kind StatusIndicatorKind, host tui.RenderRequester, spinnerColor func(string) string, messageColor func(string) string, message string, indicator *tui.LoaderIndicatorOptions) *StatusIndicator {
	return &StatusIndicator{
		Loader: tui.NewLoader(host, spinnerColor, messageColor, message, indicator),
		Kind:   kind,
	}
}

// RenderInBorder renders the message line without the leading space.
//
// D417: upstream keeps the first line the loader wraps to, so a narrow
// border cut the label at a word break ("⠋ Working (1m 16s"). When the line
// wraps, pier truncates the whole unwrapped line with an ellipsis instead
// ("⠋ Working (1m 16s · ↑3.4M ↓…"), so the cut is visible. A line that fits,
// or one so narrow that upstream keeps only the spinner, renders exactly as
// upstream's.
func (s *StatusIndicator) RenderInBorder(width int) string {
	lines := s.Loader.Render(width + 2)
	line := ""
	if len(lines) > 1 {
		line = lines[1]
	}
	if len(line) > 0 && line[0] == ' ' {
		line = line[1:]
	}
	line = trimRightSpaces(line)
	if len(lines) > 2 && tui.VisibleWidth(line) > tui.VisibleWidth(s.Loader.RenderedIndicator()) {
		full, _, _ := strings.Cut(s.Loader.Text.Text(), "\n")
		// Cut one column short and drop the spaces the cut ends on, so the
		// ellipsis follows the last visible character ("Working…", not
		// "Working …").
		kept := trimRightSpaces(tui.StripTerminalSequences(tui.TruncateToWidth(full, width-1, "", false)))
		return tui.TruncateToWidth(full, tui.VisibleWidth(kept), "", false) + "…"
	}
	return tui.TruncateToWidth(line, width, "", false)
}

// RenderSpinnerInBorder renders just the spinner frame.
func (s *StatusIndicator) RenderSpinnerInBorder(width int) string {
	return tui.TruncateToWidth(s.Loader.RenderedIndicator(), width, "", false)
}

// Dispose stops the loader.
func (s *StatusIndicator) Dispose() { s.Loader.Stop() }

// NewWorkingStatusIndicator builds the "working" spinner indicator.
func NewWorkingStatusIndicator(host tui.RenderRequester, message string, indicator *tui.LoaderIndicatorOptions, colorFn func(string) string) *StatusIndicator {
	spinnerColor := colorFn
	messageColor := colorFn
	if spinnerColor == nil {
		spinnerColor = func(text string) string { return ActiveTheme().Fg("accent", text) }
	}
	if messageColor == nil {
		messageColor = func(text string) string { return ActiveTheme().Fg("muted", text) }
	}
	return NewStatusIndicator(StatusWorking, host, spinnerColor, messageColor, message, indicator)
}

// workingTokenCount is a run's tokens in (every prompt token the provider
// processed: input, cache reads and cache writes) and out (D402).
type workingTokenCount struct {
	in, out int64
	// cacheRead is the part of in the provider served from its prompt cache.
	cacheRead int64
}

func workingTokensOf(usage ai.Usage) workingTokenCount {
	return workingTokenCount{
		in: usage.Input + usage.CacheRead + usage.CacheWrite, out: usage.Output,
		cacheRead: usage.CacheRead,
	}
}

func (c workingTokenCount) plus(other workingTokenCount) workingTokenCount {
	return workingTokenCount{
		in: c.in + other.in, out: c.out + other.out,
		cacheRead: c.cacheRead + other.cacheRead,
	}
}

func (c workingTokenCount) atLeast(other workingTokenCount) workingTokenCount {
	return workingTokenCount{
		in: max(c.in, other.in), out: max(c.out, other.out),
		cacheRead: max(c.cacheRead, other.cacheRead),
	}
}

// String is "↑48k ↓3.2k" in the footer's token format, or "" before the
// provider reports usage.
func (c workingTokenCount) String() string {
	if c.in == 0 && c.out == 0 {
		return ""
	}
	return "↑" + FormatTokens(c.in) + " ↓" + FormatTokens(c.out)
}

// cacheShare is how much of the counted input the provider served from its
// prompt cache, as `CH95%`, or "" when it reported no cache activity (a
// provider without a cache reports nothing, and 0% would be noise there).
func (c workingTokenCount) cacheShare() string {
	if c.cacheRead <= 0 || c.in <= 0 {
		return ""
	}
	share := (float64(c.cacheRead)/float64(c.in))*100 + 0.5
	if share > 100 {
		share = 100
	}
	return "CH" + strconv.Itoa(int(share)) + "%"
}

// workingElapsedLabel is the working indicator's suffix: the elapsed time in
// whole seconds, so it changes once a second, then the tokens when known:
// (12s), (1m 5s · ↑48k ↓3.2k).
func workingElapsedLabel(elapsed time.Duration, tokens workingTokenCount) string {
	label := formatWholeSeconds(int(elapsed / time.Second))
	if text := tokens.String(); text != "" {
		label += " · " + text
	}
	return "(" + label + ")"
}

func formatWholeSeconds(total int) string {
	minutes, seconds := total/60, total%60
	switch {
	case minutes == 0:
		return strconv.Itoa(seconds) + "s"
	case minutes < 60:
		return strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
	}
	return strconv.Itoa(minutes/60) + "h " + strconv.Itoa(minutes%60) + "m " + strconv.Itoa(seconds) + "s"
}

// RetryStatusIndicator shows the retry countdown.
type RetryStatusIndicator struct {
	*StatusIndicator

	countdown *CountdownTimer
}

// NewRetryStatusIndicator creates the retry indicator.
func NewRetryStatusIndicator(host tui.RenderRequester, attempt int, maxAttempts int, delayMS int) *RetryStatusIndicator {
	retryMessage := func(seconds int) string {
		return "Retrying (" + itoa(attempt) + "/" + itoa(maxAttempts) + ") in " + itoa(seconds) +
			"s... (" + KeyText("app.interrupt") + " to cancel)"
	}
	indicator := &RetryStatusIndicator{
		StatusIndicator: NewStatusIndicator(StatusRetry, host,
			func(spinner string) string { return ActiveTheme().Fg("warning", spinner) },
			func(text string) string { return ActiveTheme().Fg("muted", text) },
			retryMessage(timeoutSeconds(delayMS)), nil),
	}
	if timerHost, ok := host.(CountdownTimerHost); ok {
		indicator.countdown = NewCountdownTimer(delayMS, timerHost,
			func(seconds int) { indicator.Loader.SetMessage(retryMessage(seconds)) },
			func() { indicator.countdown = nil })
	}
	return indicator
}

// Dispose stops the indicator and the countdown.
func (r *RetryStatusIndicator) Dispose() {
	if r.countdown != nil {
		r.countdown.Dispose()
		r.countdown = nil
	}
	r.StatusIndicator.Dispose()
}

// NewCompactionStatusIndicator creates the compaction indicator. colorFn, when
// set, colors the spinner and message (the editor border color when embedded).
//
// D405: a manual run is labelled "Compacting" and an automatic one
// "Auto-compacting", each with the working indicator's elapsed tick, where
// upstream v1.5.0 status-indicator.ts CompactionStatusIndicator prints a
// sentence and a cancel hint ("Compacting context...", "Auto-compacting...",
// "Context overflow detected, Auto-compacting...", each plus "(escape to
// cancel)"). Escape still aborts compaction (handleCompactionStart swaps
// Editor.OnEscape); only the hint text and the overflow prefix are gone.
// TestCompactionIndicatorShowsOneTickingLabel checks all three reasons.
func NewCompactionStatusIndicator(host tui.RenderRequester, reason string, colorFn func(string) string) *StatusIndicator {
	label := "Compacting"
	if reason != "" && reason != "manual" {
		label = "Auto-compacting"
	}
	return newCompactionStatusIndicator(host, label, colorFn, time.Now())
}

// newCompactionStatusIndicator builds the indicator with the clock reading it
// counts from, so a test can pin the elapsed tick.
func newCompactionStatusIndicator(host tui.RenderRequester, label string, colorFn func(string) string, since time.Time) *StatusIndicator {
	spinnerColor := colorFn
	messageColor := colorFn
	if spinnerColor == nil {
		spinnerColor = func(spinner string) string { return ActiveTheme().Fg("accent", spinner) }
	}
	if messageColor == nil {
		messageColor = func(text string) string { return ActiveTheme().Fg("muted", text) }
	}
	indicator := NewStatusIndicator(StatusCompaction, host, spinnerColor, messageColor, label, nil)
	indicator.SetElapsed(since, compactionElapsedLabel)
	return indicator
}

// compactionElapsedLabel is the compaction indicator's suffix: the elapsed time
// in whole seconds, so it changes once a second (D405), e.g. (12s) or (1m 5s).
func compactionElapsedLabel(elapsed time.Duration) string {
	return "(" + formatWholeSeconds(int(elapsed/time.Second)) + ")"
}

// NewBranchSummaryStatusIndicator creates the branch summary indicator.
func NewBranchSummaryStatusIndicator(host tui.RenderRequester) *StatusIndicator {
	return NewStatusIndicator(StatusBranchSummary, host,
		func(spinner string) string { return ActiveTheme().Fg("accent", spinner) },
		func(text string) string { return ActiveTheme().Fg("muted", text) },
		"Summarizing branch... ("+KeyText("app.interrupt")+" to cancel)", nil)
}

// IdleStatus renders two empty lines.
type IdleStatus struct{}

// Invalidate drops cached state (none).
func (IdleStatus) Invalidate() {}

// Render renders the empty lines.
func (IdleStatus) Render(width int) []string {
	emptyLine := repeatSpacesLocal(width)
	return []string{emptyLine, emptyLine}
}

func repeatSpacesLocal(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(" ", count)
}

func trimRightSpaces(value string) string {
	end := len(value)
	for end > 0 && value[end-1] == ' ' {
		end--
	}
	return value[:end]
}

// BranchSummaryMessageComponent renders a branch summary with the
// collapsed/expanded state.
type BranchSummaryMessageComponent struct {
	*tui.Box

	summary       string
	markdownTheme tui.MarkdownTheme
	expanded      bool
}

// NewBranchSummaryMessageComponent creates the component.
// SetOutputPad changes the summary block's padding (upstream setOutputPad).
func (c *BranchSummaryMessageComponent) SetOutputPad(outputPad int) {
	c.Box.SetPaddingX(outputPad)
}

func NewBranchSummaryMessageComponent(summary string, markdownTheme *tui.MarkdownTheme, outputPad int) *BranchSummaryMessageComponent {
	theme := ActiveTheme()
	component := &BranchSummaryMessageComponent{
		Box:           tui.NewBox(outputPad, 1, func(text string) string { return theme.Bg("customMessageBg", text) }),
		summary:       tui.SanitizeUntrustedText(summary),
		markdownTheme: markdownThemeValue(markdownTheme),
	}
	component.updateDisplay()
	return component
}

// formatThousands formats an integer like JS's Number.toLocaleString() in the
// default (en-US) locale.
func formatThousands(value int64) string {
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(value + 1)) + 1
	} else {
		magnitude = uint64(value)
	}
	digits := strconv.FormatUint(magnitude, 10)
	var builder strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			builder.WriteByte(',')
		}
		builder.WriteRune(digit)
	}
	result := builder.String()
	if negative {
		return "-" + result
	}
	return result
}

func markdownThemeValue(theme *tui.MarkdownTheme) tui.MarkdownTheme {
	if theme != nil {
		return *theme
	}
	return GetMarkdownTheme()
}

// SetExpanded toggles the expanded state.
func (c *BranchSummaryMessageComponent) SetExpanded(expanded bool) {
	c.expanded = expanded
	c.updateDisplay()
}

// Invalidate rebuilds the content.
func (c *BranchSummaryMessageComponent) Invalidate() {
	c.Box.Invalidate()
	c.updateDisplay()
}

func (c *BranchSummaryMessageComponent) updateDisplay() {
	theme := ActiveTheme()
	c.Box.Clear()
	content := &tui.Container{}
	label := theme.Fg("customMessageLabel", "\x1b[1m[branch]\x1b[22m")
	content.AddChild(tui.NewText(label, 0, 0, nil))
	content.AddChild(tui.NewSpacer(1))
	if c.expanded {
		header := "**Branch Summary**\n\n"
		content.AddChild(tui.NewMarkdown(header+c.summary, 0, 0, c.markdownTheme,
			&tui.DefaultTextStyle{Color: func(text string) string { return theme.Fg("customMessageText", text) }},
			tui.MarkdownOptions{}))
	} else {
		content.AddChild(tui.NewText(
			theme.Fg("customMessageText", "Branch summary (")+
				theme.Fg("dim", KeyText("app.tools.expand"))+
				theme.Fg("customMessageText", " to expand)"), 0, 0, nil))
	}
	c.Box.AddChild(tui.NewMouseRegion(content, func(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult {
		if event.Type != tui.MouseClick || event.Button != tui.MouseButtonLeft {
			return nil
		}
		c.SetExpanded(!c.expanded)
		return &tui.TuiMouseDispatchResult{Handled: true}
	}))
}

// CompactionSummaryMessageComponent renders a compaction summary.
type CompactionSummaryMessageComponent struct {
	*tui.Box

	summary       string
	tokensBefore  int64
	markdownTheme tui.MarkdownTheme
	expanded      bool
}

// NewCompactionSummaryMessageComponent creates the component.
// SetOutputPad changes the summary block's padding (upstream setOutputPad).
func (c *CompactionSummaryMessageComponent) SetOutputPad(outputPad int) {
	c.Box.SetPaddingX(outputPad)
}

func NewCompactionSummaryMessageComponent(summary string, tokensBefore int64, markdownTheme *tui.MarkdownTheme, outputPad int) *CompactionSummaryMessageComponent {
	theme := ActiveTheme()
	component := &CompactionSummaryMessageComponent{
		Box:           tui.NewBox(outputPad, 1, func(text string) string { return theme.Bg("customMessageBg", text) }),
		summary:       tui.SanitizeUntrustedText(summary),
		tokensBefore:  tokensBefore,
		markdownTheme: markdownThemeValue(markdownTheme),
	}
	component.updateDisplay()
	return component
}

// SetExpanded toggles the expanded state.
func (c *CompactionSummaryMessageComponent) SetExpanded(expanded bool) {
	c.expanded = expanded
	c.updateDisplay()
}

// Invalidate rebuilds the content.
func (c *CompactionSummaryMessageComponent) Invalidate() {
	c.Box.Invalidate()
	c.updateDisplay()
}

func (c *CompactionSummaryMessageComponent) updateDisplay() {
	theme := ActiveTheme()
	c.Box.Clear()
	content := &tui.Container{}
	label := theme.Fg("customMessageLabel", "\x1b[1m[compaction]\x1b[22m")
	content.AddChild(tui.NewText(label, 0, 0, nil))
	content.AddChild(tui.NewSpacer(1))
	tokenStr := formatThousands(c.tokensBefore)
	if c.expanded {
		header := "**Compacted from " + tokenStr + " tokens**\n\n"
		content.AddChild(tui.NewMarkdown(header+c.summary, 0, 0, c.markdownTheme,
			&tui.DefaultTextStyle{Color: func(text string) string { return theme.Fg("customMessageText", text) }},
			tui.MarkdownOptions{}))
	} else {
		content.AddChild(tui.NewText(
			theme.Fg("customMessageText", "Compacted from "+tokenStr+" tokens (")+
				theme.Fg("dim", KeyText("app.tools.expand"))+
				theme.Fg("customMessageText", " to expand)"), 0, 0, nil))
	}
	c.Box.AddChild(tui.NewMouseRegion(content, func(event tui.TuiMouseEvent) *tui.TuiMouseDispatchResult {
		if event.Type != tui.MouseClick || event.Button != tui.MouseButtonLeft {
			return nil
		}
		c.SetExpanded(!c.expanded)
		return &tui.TuiMouseDispatchResult{Handled: true}
	}))
}
