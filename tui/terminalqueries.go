package tui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Port of the terminal query protocol of upstream's TuiBase: the OSC 11
// background-color query with its pending-query bookkeeping, the `CSI ? 996` /
// `CSI ? 997` color-scheme pair, the listener registries that deliver replies,
// and the `CSI ? 2031` notification toggle (D165).
//
// It is deliberately independent of the renderer: nothing here paints or
// dispatches input, the writer arrives as a one-method seam on each call, and the
// reply path is driven by ConsumeInput. The renderer holds one and forwards its
// own methods to it; tests drive the protocol with a fake writer alone.

// terminalQueryWriter is the write half of a terminal, and the seam the query
// protocol writes through.
type terminalQueryWriter interface {
	Write(data string)
}

// pendingOSC11Query is one in-flight OSC 11 query.
type pendingOSC11Query struct {
	settled bool
	result  chan osc11Result
}

type osc11Result struct {
	color RgbColor
	ok    bool
}

type colorSchemeListener struct {
	id       int
	listener func(TerminalColorScheme)
}

type backgroundListener struct {
	id       int
	listener func(RgbColor)
}

// terminalQueries owns the query protocol's state: the pending OSC 11 queries,
// the reply registries and the notification toggle. Registration and reply
// dispatch both happen on the owner loop, so the registries need no lock.
type terminalQueries struct {
	// started/stopped mirror the terminal's lifecycle: a notification toggle
	// requested before Start only flips the flag, and Start replays it once raw
	// mode is active, so the mode sequence is never echoed into the input stream
	// (the SSH-launch freeze, D165).
	started bool
	stopped bool
	notify  bool

	pendingOSC11Replies  int
	pendingOSC11Queries  []*pendingOSC11Query
	colorSchemeListeners []colorSchemeListener
	nextColorSchemeID    int
	backgroundListeners  []backgroundListener
	nextBackgroundID     int

	// colorMu guards the terminal-colors query queue, which the blocking caller
	// appends to while the owner loop's input dispatch reads it.
	colorMu                     sync.Mutex
	pendingTerminalColorQueries []*pendingTerminalColorQuery
}

const (
	terminalPaletteSize     = 16
	terminalColorReplyCount = 2 + terminalPaletteSize
)

// terminalColorQuery asks for the default foreground (OSC 10), background
// (OSC 11) and all 16 palette colors (OSC 4), ended by a DA1 request that marks
// the end of the replies (upstream TERMINAL_COLOR_QUERY).
var terminalColorQuery = func() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x07\x1b]11;?\x07")
	for index := 0; index < terminalPaletteSize; index++ {
		fmt.Fprintf(&b, "\x1b]4;%d;?\x07", index)
	}
	b.WriteString("\x1b[c")
	return b.String()
}()

// pendingTerminalColorQuery is one in-flight terminal-colors query.
type pendingTerminalColorQuery struct {
	foreground *RgbColor
	background *RgbColor
	palette    [terminalPaletteSize]*RgbColor
	replied    map[string]bool
	deliver    func(TerminalColors)
}

// OnBackgroundChange subscribes to OSC 11 background-color replies. The listener
// runs on the owner loop (input dispatch); registration happens during
// setup/teardown only, so the registry needs no lock.
func (q *terminalQueries) OnBackgroundChange(listener func(RgbColor)) func() {
	q.nextBackgroundID++
	id := q.nextBackgroundID
	q.backgroundListeners = append(q.backgroundListeners, backgroundListener{id: id, listener: listener})
	return func() {
		filtered := q.backgroundListeners[:0]
		for _, entry := range q.backgroundListeners {
			if entry.id != id {
				filtered = append(filtered, entry)
			}
		}
		q.backgroundListeners = filtered
	}
}

// OnColorSchemeChange subscribes to terminal color-scheme reports.
func (q *terminalQueries) OnColorSchemeChange(listener func(TerminalColorScheme)) func() {
	q.nextColorSchemeID++
	id := q.nextColorSchemeID
	q.colorSchemeListeners = append(q.colorSchemeListeners, colorSchemeListener{id: id, listener: listener})
	return func() {
		filtered := q.colorSchemeListeners[:0]
		for _, entry := range q.colorSchemeListeners {
			if entry.id != id {
				filtered = append(filtered, entry)
			}
		}
		q.colorSchemeListeners = filtered
	}
}

// RequestBackground writes the OSC 11 query and returns. It never blocks: the
// reply is delivered to the OnBackgroundChange listeners on the owner loop.
// Callers must invoke it only after the terminal is in raw mode and the input
// reader is live, so the reply is dispatched rather than echoed (D165).
func (q *terminalQueries) RequestBackground(writer terminalQueryWriter) {
	if writer == nil || q.stopped {
		return
	}
	writer.Write("\x1b]11;?\x07")
}

// QueryBackground queries the terminal's background color with OSC 11. It returns
// ok=false on timeout or an unparsable reply.
func (q *terminalQueries) QueryBackground(writer terminalQueryWriter, timeoutMS int) (RgbColor, bool) {
	query := &pendingOSC11Query{result: make(chan osc11Result, 1)}
	q.pendingOSC11Queries = append(q.pendingOSC11Queries, query)
	q.pendingOSC11Replies++
	if writer == nil {
		return RgbColor{}, false
	}
	writer.Write("\x1b]11;?\x07")

	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case result := <-query.result:
		return result.color, result.ok
	case <-timer.C:
		if !query.settled {
			query.settled = true
		}
		return RgbColor{}, false
	}
}

// QueryTerminalColors queries the terminal's default foreground and background
// and its 16-color palette in one write. It resolves when the DA1 reply or every
// color reply arrives, or after timeoutMS; replies that arrive later go to
// onLateReply (upstream queryTerminalColors).
func (q *terminalQueries) QueryTerminalColors(writer terminalQueryWriter, timeoutMS int, onLateReply func(TerminalColors)) TerminalColors {
	if q.stopped {
		return TerminalColors{}
	}
	query := &pendingTerminalColorQuery{palette: [terminalPaletteSize]*RgbColor{}, replied: map[string]bool{}}
	result := make(chan TerminalColors, 1)
	query.deliver = func(colors TerminalColors) {
		select {
		case result <- colors:
		default:
		}
	}
	q.colorMu.Lock()
	q.pendingTerminalColorQueries = append(q.pendingTerminalColorQueries, query)
	q.colorMu.Unlock()
	if writer != nil {
		writer.Write(terminalColorQuery)
	}
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case colors := <-result:
		return colors
	case <-timer.C:
		q.colorMu.Lock()
		query.deliver = onLateReply
		colors := terminalColorQueryResult(query)
		q.colorMu.Unlock()
		return colors
	}
}

// consumeTerminalColorResponse routes a color reply or DA1 to the oldest pending
// terminal-colors query.
func (q *terminalQueries) consumeTerminalColorResponse(data string) bool {
	q.colorMu.Lock()
	defer q.colorMu.Unlock()
	if len(q.pendingTerminalColorQueries) == 0 {
		return false
	}
	query := q.pendingTerminalColorQueries[0]
	if deviceAttributesResponsePattern.MatchString(data) {
		q.pendingTerminalColorQueries = q.pendingTerminalColorQueries[1:]
		q.completeTerminalColorQueryLocked(query)
		return true
	}
	response, ok := ParseOscColorResponse(data)
	if !ok {
		return false
	}
	key := "palette:" + strconv.Itoa(response.Target.Index)
	switch {
	case response.Target.Foreground:
		key = "foreground"
	case response.Target.Background:
		key = "background"
	}
	if query.deliver == nil || query.replied[key] {
		return true
	}
	query.replied[key] = true
	switch {
	case response.Target.Foreground:
		query.foreground = terminalColorResponseRGB(response)
	case response.Target.Background:
		query.background = terminalColorResponseRGB(response)
	case response.Target.Index >= 0 && response.Target.Index < terminalPaletteSize:
		query.palette[response.Target.Index] = terminalColorResponseRGB(response)
	}
	if len(query.replied) == terminalColorReplyCount {
		q.completeTerminalColorQueryLocked(query)
	}
	return true
}

func terminalColorResponseRGB(response OscColorResponse) *RgbColor {
	if !response.HasRGB {
		return nil
	}
	color := response.RGB
	return &color
}

func terminalColorQueryResult(query *pendingTerminalColorQuery) TerminalColors {
	colors := TerminalColors{Foreground: query.foreground, Background: query.background}
	palette := make([]RgbColor, 0, terminalPaletteSize)
	for _, color := range query.palette {
		if color == nil {
			return colors
		}
		palette = append(palette, *color)
	}
	colors.Palette = palette
	return colors
}

func (q *terminalQueries) completeTerminalColorQueryLocked(query *pendingTerminalColorQuery) {
	deliver := query.deliver
	query.deliver = nil
	if deliver != nil {
		deliver(terminalColorQueryResult(query))
	}
}

// QueryColorScheme queries the terminal's color-scheme preference with DSR
// (`CSI ? 996 n`).
func (q *terminalQueries) QueryColorScheme(writer terminalQueryWriter, timeoutMS int) (TerminalColorScheme, bool) {
	results := make(chan TerminalColorScheme, 1)
	unsubscribe := q.OnColorSchemeChange(func(scheme TerminalColorScheme) {
		select {
		case results <- scheme:
		default:
		}
	})
	defer unsubscribe()
	if writer == nil {
		return "", false
	}
	writer.Write("\x1b[?996n")

	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case scheme := <-results:
		return scheme, true
	case <-timer.C:
		return "", false
	}
}

// SetNotify enables the `CSI ? 2031` notifications.
func (q *terminalQueries) SetNotify(writer terminalQueryWriter, enabled bool) {
	if q.notify == enabled {
		return
	}
	q.notify = enabled
	// Before the terminal is started the request only flips the flag: Start
	// replays it once raw mode is active, so the sequence is never echoed into
	// the input stream (the SSH-launch freeze).
	if !q.started {
		return
	}
	if q.stopped || writer == nil {
		return
	}
	if enabled {
		writer.Write("\x1b[?2031h")
	} else {
		writer.Write("\x1b[?2031l")
	}
}

// NotifyOnStart replays a notification toggle requested before the terminal was
// started, and marks it started.
func (q *terminalQueries) NotifyOnStart(writer terminalQueryWriter) {
	q.started = true
	if q.notify && writer != nil {
		writer.Write("\x1b[?2031h")
	}
}

// NotifyOnStop disables an enabled notification and marks the terminal stopped.
func (q *terminalQueries) NotifyOnStop(writer terminalQueryWriter) {
	started := q.started
	q.started = false
	q.stopped = true
	if started && q.notify {
		q.notify = false
		if writer != nil {
			writer.Write("\x1b[?2031l")
		}
	}
}

// ConsumeInput resolves an OSC 11 reply or a color-scheme report out of the input
// stream, reporting whether it consumed it. An OSC 11 reply is never user input,
// so it is consumed even when no query is pending; otherwise a proactive probe's
// reply would leak into the editor.
func (q *terminalQueries) ConsumeInput(data string) bool {
	if q.consumeTerminalColorResponse(data) {
		return true
	}
	if IsOsc11BackgroundColorResponse(data) {
		q.resolveBackgroundResponse(data)
		return true
	}
	scheme, ok := ParseTerminalColorSchemeReport(data)
	if !ok {
		return false
	}
	listeners := append([]colorSchemeListener{}, q.colorSchemeListeners...)
	for _, entry := range listeners {
		entry.listener(scheme)
	}
	return true
}

// resolveBackgroundResponse resolves the oldest pending OSC 11 query and notifies
// the background listeners.
func (q *terminalQueries) resolveBackgroundResponse(data string) {
	color, ok := ParseOsc11BackgroundColor(data)

	if q.pendingOSC11Replies > 0 {
		q.pendingOSC11Replies--
		var query *pendingOSC11Query
		if len(q.pendingOSC11Queries) > 0 {
			query = q.pendingOSC11Queries[0]
			q.pendingOSC11Queries = q.pendingOSC11Queries[1:]
		}
		if query != nil && !query.settled {
			query.settled = true
			select {
			case query.result <- osc11Result{color: color, ok: ok}:
			default:
			}
		}
	}

	if ok && len(q.backgroundListeners) > 0 {
		listeners := append([]backgroundListener{}, q.backgroundListeners...)
		for _, entry := range listeners {
			entry.listener(color)
		}
	}
}
