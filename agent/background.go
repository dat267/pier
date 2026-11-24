package agent

// Automatic backgrounding of long tool calls. A foreground call that outlives
// the harness's threshold keeps running instead of freezing the turn: the model
// receives a handle, the process's output keeps going to a file, and one
// notification arrives when it ends.
//
// The decision is the harness's alone. No tool argument, key binding or user
// request moves a call, and nothing lets a caller demand it: the threshold is
// the whole policy.
//
// The pinned reference runs every tool call to completion, so this is pier-only
// (D538). It adds no transcript shape: the handle is an ordinary tool result and
// the completion notification is an ordinary user message, both already readable
// by the reference.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
)

// ErrBackgrounded marks the outcome whose result is a handle rather than a
// finished call.
var ErrBackgrounded = errors.New("tool call backgrounded")

// backgroundRegistry owns the calls this session moved to the background, so
// they can be cancelled when the session closes and so each reports exactly once.
// A displaced call outlives the turn that started it, and the run, which is the
// whole point; it does not outlive the session.
type backgroundRegistry struct {
	mu    sync.Mutex
	calls []*backgroundCall
	// closed stops new registrations once the run is over.
	closed bool
}

// backgroundCall is one displaced tool call.
type backgroundCall struct {
	handle   ToolCallHandle
	cancel   context.CancelFunc
	finished chan struct{}
	// result is written before finished closes, and read only after.
	result   AgentToolResult
	once     sync.Once
	reported sync.Once
}

// newBackgroundRegistry creates an empty registry.
func newBackgroundRegistry() *backgroundRegistry { return &backgroundRegistry{} }

// backgroundRegistry returns the agent's registry, creating it on first use.
func (a *Agent) backgroundRegistry() *backgroundRegistry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.backgroundCalls == nil {
		a.backgroundCalls = newBackgroundRegistry()
	}
	return a.backgroundCalls
}

// CloseBackgroundCalls stops every displaced tool call. It is the session's job
// to call it: a displaced call may outlive its turn and its run, never the
// session.
func (a *Agent) CloseBackgroundCalls() {
	a.mu.Lock()
	registry := a.backgroundCalls
	a.backgroundCalls = nil
	a.mu.Unlock()
	if registry != nil {
		registry.close()
	}
}

// register adds a displaced call, or refuses one when the run is already over.
func (r *backgroundRegistry) register(call *backgroundCall) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.calls = append(r.calls, call)
	return true
}

// unregister drops a call that finished, so a long run does not accumulate them.
func (r *backgroundRegistry) unregister(call *backgroundCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, candidate := range r.calls {
		if candidate == call {
			r.calls = append(r.calls[:index], r.calls[index+1:]...)
			return
		}
	}
}

// close cancels every displaced call and refuses new ones. A backgrounded call
// outlives the turn that started it but not the run: a settled run must leave
// nothing behind.
func (r *backgroundRegistry) close() {
	r.mu.Lock()
	r.closed = true
	calls := r.calls
	r.calls = nil
	r.mu.Unlock()
	for _, call := range calls {
		call.cancel()
	}
}

// active reports how many displaced calls are still running, for tests and
// teardown assertions.
func (r *backgroundRegistry) active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// outputFilePath names the output file, tolerating a host that supplies no path
// function: the handle then reports no path rather than panicking.
func (p *backgroundPolicy) outputFilePath(toolCallID string) string {
	if p.outputPath == nil {
		return ""
	}
	return p.outputPath(toolCallID)
}

// backgroundOutputPath is where a displaced call's output goes. The host supplies
// it, so the file lives under the agent's own temporary directory rather than the
// working tree.
type backgroundOutputPath func(toolCallID string) string

// backgroundHandle describes the displaced call to the model. It is an ordinary
// tool result: the call has not failed, so it is not an error, and the text says
// plainly what happened and where the output is.
func backgroundHandle(handle ToolCallHandle, threshold int64) AgentToolResult {
	output := handle.OutputPath
	if output == "" {
		output = "a temporary file"
	}
	text := fmt.Sprintf(
		"This call is still running after %d seconds, so it was moved to the background as %s.\n\n"+
			"Its output is being written to %s. You will be told once, when it finishes.\n\n"+
			"Continue with other work; do not wait for it, and do not call it again.",
		threshold, handle.ID, output)
	return AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: text}},
		Details: backgroundDetails(handle),
	}
}

// backgroundDetails is the handle's structured detail, so a renderer can show it
// without parsing the text.
func backgroundDetails(handle ToolCallHandle) json.RawMessage {
	// Marshal through ai.MarshalJSON so key order and escaping match the rest of
	// the wire (AGENTS.md: final bytes never come from encoding/json).
	encoded, err := ai.MarshalJSON(struct {
		Handle     string `json:"backgroundHandle"`
		ToolCallID string `json:"toolCallId"`
		ToolName   string `json:"toolName"`
		OutputPath string `json:"outputPath"`
	}{handle.ID, handle.ToolCallID, handle.ToolName, handle.OutputPath})
	if err != nil {
		return nil
	}
	return encoded
}

// newBackgroundHandleID builds a short handle id. It is not a session id and
// never enters the transcript as one: it labels a tool result's text and detail.
func newBackgroundHandleID() string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "bg"
	}
	return "bg-" + hex.EncodeToString(raw[:])
}

// CompletionNotice is the message a finished backgrounded call wakes the model
// with: an ordinary user message, so the transcript keeps a shape the reference
// can already read.
func CompletionNotice(handle ToolCallHandle, result AgentToolResult) ai.Message {
	status := "finished"
	if result.IsError {
		status = "failed"
	}
	var body string
	for _, block := range result.Content {
		if text, ok := block.(ai.TextContent); ok {
			body += text.Text
		}
	}
	if body == "" {
		body = "(no output)"
	}
	// Name the file, so the agent can read output the notice does not carry, and
	// cap the body so one long call cannot flood the transcript it wakes.
	where := ""
	if handle.OutputPath != "" {
		where = fmt.Sprintf(" The full output is in %s.", handle.OutputPath)
	}
	text := fmt.Sprintf("Background call %s (%s) %s.%s\n\n%s",
		handle.ID, handle.ToolName, status, where, clipBackgroundBody(body))
	return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}}
}

// backgroundNoticeBodyLimit caps the result text a wake-up carries. A call that
// produced more than this leaves the rest in its output file, which the notice
// names, so nothing is lost.
const backgroundNoticeBodyLimit = 2000

// clipBackgroundBody keeps the tail of the body when it is over the limit: a long
// call's end is what matters, and the file holds the rest.
func clipBackgroundBody(body string) string {
	if len(body) <= backgroundNoticeBodyLimit {
		return body
	}
	// Start on a character boundary, so the cut never splits one.
	start := len(body) - backgroundNoticeBodyLimit
	for start < len(body) && !utf8.RuneStart(body[start]) {
		start++
	}
	kept := body[start:]
	return "...(cut; the full output is in the file named above)\n" + kept
}

// executeWithBackground runs the call and gives up on waiting once the threshold
// expires. It reports true when it has an outcome for the caller: either a handle
// for a call that outlived the threshold, or the result of one that finished
// first. Both mean the tool has already executed, so the caller must return the
// outcome rather than run the call a second time. It reports false only when there
// is nothing to return, which today is the aborted-cancellation case.
func executeWithBackground(
	ctx context.Context,
	prepared *preparedToolCallValue,
	emit AgentEventSink,
	policy *backgroundPolicy,
) (executedToolCallOutcome, bool) {
	updates := newToolUpdateDispatcher(emit)
	startedAt := time.Now()

	// The call gets its own lifetime. Tying it to ctx would kill a backgrounded
	// process the moment the turn that started it settled, which is the opposite
	// of the point; the registry cancels it when the whole run ends.
	callCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	type completion struct {
		result AgentToolResult
		err    error
	}
	// The output file is opened before the call starts, not when the handle is
	// built: a call that streams progress must be readable while it runs, which is
	// the whole point of handing the agent a path. It is truncated once here so a
	// reused path cannot show a previous call's output.
	outputPath := policy.outputFilePath(prepared.toolCall.ID)
	output := newBackgroundOutput(outputPath)

	done := make(chan completion, 1)
	go func() {
		var result AgentToolResult
		var err error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = errors.New(panicMessage(recovered))
				}
			}()
			result, err = prepared.tool.Execute(callCtx, prepared.toolCall.ID, prepared.args, func(partialResult AgentToolResult) {
				// Stream the piece into the file as it arrives, so a long call's
				// progress is readable from the handle rather than only its end.
				output.write(backgroundPieceText(partialResult))
				updates.send(AgentEvent{
					Type: ToolExecutionUpdate, ToolCallID: prepared.toolCall.ID,
					ToolName: prepared.toolCall.Name, Args: prepared.toolCall.Arguments,
					PartialResult: partialResult,
				})
			})
		}()
		done <- completion{result: result, err: err}
	}()

	timer := time.NewTimer(policy.threshold)
	defer timer.Stop()
	select {
	case finished := <-done:
		// Finished in time: hand back exactly what the foreground path produces,
		// and drop the scratch file the call never needed.
		cancel()
		output.discard()
		durationMS := roundDurationMilliseconds(time.Since(startedAt))
		if errFromUpdate := updates.close(); errFromUpdate != nil {
			panic(errFromUpdate)
		}
		if finished.err == nil {
			return executedToolCallOutcome{result: finished.result, isError: finished.result.IsError, durationMS: durationMS}, true
		}
		if ctxDone(ctx) && isContextError(finished.err) {
			finished.err = errors.New("Operation aborted")
		}
		return executedToolCallOutcome{
			result: createErrorToolResult(finished.err.Error()), isError: true, durationMS: durationMS,
		}, true
	case <-timer.C:

	}

	// Outlived the threshold: keep the call running and return a handle.
	call := &backgroundCall{cancel: cancel, finished: make(chan struct{})}
	handle := ToolCallHandle{
		ID:         policy.nextID(),
		ToolCallID: prepared.toolCall.ID,
		ToolName:   prepared.toolCall.Name,
		OutputPath: outputPath,
	}
	handle.Wait = func() AgentToolResult {
		<-call.finished
		return call.result
	}
	call.handle = handle

	// Record the result and let whoever waits read it, exactly once.
	go func() {
		finished := <-done
		if finished.err != nil {
			output.writeFailure(finalOutputText(finished.result, finished.err))
		} else {
			output.write(finalOutputText(finished.result, finished.err))
		}
		output.close()
		if finished.err == nil {
			call.result = finished.result
		} else {
			// createErrorToolResult builds the text; the error flag is the caller's
			// to set, as it is on the foreground path.
			failure := createErrorToolResult(finished.err.Error())
			failure.IsError = true
			call.result = failure
		}
		// Unregister before signalling completion, so a caller that Wait returns to
		// never observes the call as still running.
		policy.registry.unregister(call)
		if errFromUpdate := updates.close(); errFromUpdate != nil {
			_ = errFromUpdate
		}
		call.once.Do(func() { close(call.finished) })
	}()
	registered := policy.registry != nil && policy.registry.register(call)
	if !registered {
		cancel()
		return executedToolCallOutcome{
			result: createErrorToolResult("Operation aborted"), isError: true,
		}, false
	}
	if policy.onRegister != nil {
		policy.onRegister(handle)
	}
	threshold := int64(policy.threshold / time.Second)
	// The duration is the call's time in the foreground, so its frame reads
	// "Took 30.0s" rather than 0.0s next to "still running after 30 seconds".
	return executedToolCallOutcome{
		result: backgroundHandle(handle, threshold), durationMS: roundDurationMilliseconds(time.Since(startedAt)),
	}, true
}

// backgroundOutput is the file a displaced call's output accumulates in. A
// failure to open it is not fatal: the call still runs, and the handle simply
// reports no path.
type backgroundOutput struct {
	mu   sync.Mutex
	file *os.File
	path string
	// last is what the file holds, so a failure can keep the output before it.
	last string
}

// newBackgroundOutput opens the file, or returns one that discards.
func newBackgroundOutput(path string) *backgroundOutput {
	if path == "" {
		return &backgroundOutput{}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return &backgroundOutput{path: path}
	}
	return &backgroundOutput{file: file, path: path}
}

// write replaces the file's contents with text, if there is a file. A partial
// result is the whole result so far, not a delta (upstream's interactive mode
// replaces the shown result with each one), so appending would repeat
// everything already written. An empty piece keeps what the file holds.
func (o *backgroundOutput) write(text string) { o.replace(text, false) }

// writeFailure records a failed call's error. A shell's error already starts
// with its output and replaces the file like a result; an error that does not
// is added after the output streamed so far, so a failure never loses it.
func (o *backgroundOutput) writeFailure(text string) { o.replace(text, true) }

func (o *backgroundOutput) replace(text string, failed bool) {
	if o == nil || text == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file == nil {
		return
	}
	if failed && !strings.HasPrefix(text, strings.TrimSuffix(o.last, "\n")) {
		text = o.last + text
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_ = o.file.Truncate(0)
	_, _ = o.file.WriteAt([]byte(text), 0)
	o.last = text
}

// close releases the file.
func (o *backgroundOutput) close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file != nil {
		_ = o.file.Close()
		o.file = nil
	}
}

// finalOutputText is what the output file ends with: the call's result, or its
// failure.
func finalOutputText(result AgentToolResult, err error) string {
	if err != nil {
		return err.Error()
	}
	var body strings.Builder
	for _, block := range result.Content {
		if text, ok := block.(ai.TextContent); ok {
			body.WriteString(text.Text)
		}
	}
	return body.String()
}

// backgroundPieceText is the text of one streamed update. A piece with no text
// (an image-only update) writes nothing rather than a blank line.
func backgroundPieceText(result AgentToolResult) string {
	var body strings.Builder
	for _, block := range result.Content {
		if text, ok := block.(ai.TextContent); ok {
			body.WriteString(text.Text)
		}
	}
	return body.String()
}

// discard releases the file and removes it, for a call that finished inside the
// threshold and so never needed an output file.
func (o *backgroundOutput) discard() {
	if o == nil {
		return
	}
	o.mu.Lock()
	path := o.path
	if o.file != nil {
		_ = o.file.Close()
		o.file = nil
	}
	o.mu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
}
