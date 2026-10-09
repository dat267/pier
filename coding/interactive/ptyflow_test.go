package interactive

// Functional replacements for the PTY flow tests removed with the D136-D139
// watchdog suite (see ptywatch_test.go's header and docs/locks.md): driving the
// real binary through a pty proved the mutex-deadlock class cannot happen, but
// the stage-4 refactor retired every UI mutex, so the class is structurally
// gone and the same flows assert their *functional* behavior here instead, on
// the app's own loop, in milliseconds.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// runTestAppLoop starts the app's main input loop and returns a stop function.
// The loop owns the containers and the deadline mirrors TestAppEndToEndLoop's.
func runTestAppLoop(t *testing.T, app *App) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Run(ctx)
	}()
	waitForConditionWithin(t, func() bool { return app.lifecycle.IsInitialized() }, 45*time.Second)
	waitForConditionWithin(t, func() bool {
		return app.runner != nil && app.runner.LoopBeats() > 0
	}, 45*time.Second)
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("run loop did not exit after cancellation")
		}
	}
}

// sessionHasUserMessage reports whether the session recorded text as a user
// message. This is the submit half of the old TestPTYEditorSubmitNoMutexDeadlock:
// typing and pressing Enter must reach the session.
func sessionHasUserMessage(app *App, text string) bool {
	for _, message := range app.session.Messages() {
		if user, ok := message.(*ai.UserMessage); ok {
			if ai.ContentText(user.Content, "") == text {
				return true
			}
		}
	}
	return false
}

// waitForPromptRecorded waits for the session to record a submitted prompt. On timeout
// it prints the chat first: a turn that failed only leaves its error in the transcript,
// so a failed turn and a slow one look identical to a poll over the session, and the
// error text is what identifies the failure.
func waitForPromptRecorded(t *testing.T, app *App, prompt string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	nextChatCheck := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sessionHasUserMessage(app, prompt) {
			return
		}
		// A turn that failed adds its error to the transcript, and the session then never
		// records the prompt, so polling the session alone cannot tell a failed turn from a
		// slow one. Checking the chat is the expensive half, so it runs at its own cadence.
		if time.Now().After(nextChatCheck) {
			nextChatCheck = time.Now().Add(250 * time.Millisecond)
			if chat := chatText(app); strings.Contains(chat, "Error: ") {
				t.Fatalf("the prompt was not recorded; the turn failed:\n%s", chat)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Logf("chat at timeout:\n%s", chatText(app))
	// Report through the shared helper, which adds the goroutine dump.
	waitForConditionWithin(t, func() bool { return sessionHasUserMessage(app, prompt) }, time.Millisecond)
}

// chatText renders the chat on the loop goroutine, where the containers live (D146).
func chatText(app *App) string {
	return postValue(app, func() string {
		return coding.StripAnsi(strings.Join(app.chat.Render(120), "\n"))
	})
}

// TestSubmitFromEditorReachesSession covers what the editor-submit PTY flow
// asserted functionally: a submitted prompt is recorded in the session and the
// model's reply renders, with no pty, no binary spawn and no SIGQUIT.
func TestSubmitFromEditorReachesSession(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	const prompt = "hello from the submit replacement"
	app.startup.QueueUserInput(prompt)

	waitForPromptRecorded(t, app, prompt)
	// The faux stream replies "ack"; the reply must reach the session too, which
	// is what the PTY flow's "did not exit / still responsive" assertion checked
	// indirectly.
	waitForConditionWithin(t, func() bool {
		for _, message := range app.session.Messages() {
			if assistant, ok := message.(*ai.AssistantMessage); ok {
				if ai.ContentText(ai.StringOrBlocks{Blocks: assistant.Content}, "") == "ack" {
					return true
				}
			}
		}
		return false
	}, 30*time.Second)
}

// TestQuitCommandShutsDown covers the exit half of the old
// TestPTYExitNoMutexDeadlock: /quit must request a shutdown, and the loop must
// act on it. The PTY flow's 5s exit deadline becomes a poll on the lifecycle.
func TestQuitCommandShutsDown(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	app.submit.HandleSubmit(context.Background(), "/quit")
	waitForConditionWithin(t, app.lifecycle.IsShuttingDown, 10*time.Second)
}

// TestModelSelectorOpensAndCloses covers the model-selector PTY flow's
// functional shape: /model replaces the editor with the selector and cancelling
// restores it. The mutex assertion that flow carried is gone with the locks
// (docs/locks.md); what remains worth pinning is that the slot round-trips and
// the editor comes back.
func TestModelSelectorOpensAndCloses(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	// The selector is installed on the UI loop, so drive the submit there too.
	app.ui.Post(func() { app.submit.HandleSubmit(context.Background(), "/model") })
	waitForConditionWithin(t, func() bool {
		return postValue(app, app.slot.HasActiveSelector)
	}, 10*time.Second)

	// Cancelling runs the selector's done callback, which restores the editor
	// (upstream showSelector's done; disposeActiveSelector only tears down).
	selector, ok := postValue(app, app.slot.ActiveSelectorComponent).(*ModelSelectorComponent)
	if !ok {
		t.Fatalf("active selector = %T", postValue(app, app.slot.ActiveSelectorComponent))
	}
	app.ui.Post(func() { selector.onCancel() })
	waitForConditionWithin(t, func() bool {
		return !postValue(app, app.slot.HasActiveSelector)
	}, 10*time.Second)
	// The done callback restores the editor as the editor container's child.
	restored := postValue(app, func() bool {
		return len(app.editorContainer.Children) == 1 && app.editorContainer.Children[0] == app.defaultEditor
	})
	if !restored {
		t.Fatal("editor not restored after the selector was cancelled")
	}
}

// TestTranscriptScrollsWithoutStalling covers the scroll PTY flow's functional
// shape: scrolling a filled transcript moves the viewport off the end and back.
// The old flow's mutex assertion is gone with the locks (docs/locks.md); this
// pins the observable scroll state instead of "nothing panicked".
func TestTranscriptScrollsWithoutStalling(t *testing.T) {
	// Fullscreen: the transcript scroll view is laid out by the fullscreen
	// renderer, which is the flow the old PTY test drove.
	app, cleanup := newTestAppB(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	// Overflow the transcript so there is somewhere to scroll: a ScrollView's
	// range only exists after a layout pass fixes its content and viewport
	// heights. Events go through the producer queue the loop drains, so the test
	// never touches a container the loop is rendering.
	for i := 0; i < 60; i++ {
		app.sessionEvents.enqueue(&coding.SessionEvent{
			Type: coding.SessionMessageStart,
			Agent: agentEvent("message_start", &ai.UserMessage{
				Content: ai.StringOrBlocks{Text: fmt.Sprintf("scrollable transcript line %d with enough text to wrap across a couple of terminal lines", i)},
			}),
		})
		app.sessionEvents.enqueue(&coding.SessionEvent{
			Type:  coding.SessionMessageEnd,
			Agent: agentEvent("message_end", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "x"}}),
		})
	}

	scroll := app.transcriptScrollView
	if scroll == nil {
		t.Skip("no transcript scroll view in this renderer")
	}

	// Wait for the loop to apply the events first. That is a state signal, where waiting on the
	// layout height alone raced the suite's load: applying sixty messages is the expensive part,
	// and the height check then failed inside its budget while the work was still being done.
	waitForConditionWithin(t, func() bool {
		return postValue(app, func() int { return len(app.chat.Children) }) >= 60
	}, 60*time.Second)

	// The loop lays the transcript out on its next beat; the scroll range only
	// exists once the viewport has a height.
	waitForConditionWithin(t, func() bool {
		return postValue(app, scroll.ViewportHeight) > 0
	}, 30*time.Second)

	// Scrolling to the top on the loop must leave the end. The callback only
	// runs when the loop is draining, which is the "no stall" half of the old
	// flow.
	app.ui.Post(scroll.ScrollToStart)
	waitForConditionWithin(t, func() bool {
		return !postValue(app, scroll.IsFollowingEnd)
	}, 10*time.Second)

	// Scrolling back to the end restores follow mode.
	app.ui.Post(scroll.ScrollToEnd)
	waitForConditionWithin(t, func() bool {
		return postValue(app, scroll.IsFollowingEnd)
	}, 10*time.Second)
}

// postValue runs a read on the UI loop and returns its value, so the test never
// reads component state the loop owns. A stalled loop leaves the zero value.
func postValue[T any](app *App, read func() T) T {
	result := make(chan T, 1)
	app.ui.Post(func() { result <- read() })
	select {
	case value := <-result:
		return value
	case <-time.After(2 * time.Second):
		var zero T
		return zero
	}
}
