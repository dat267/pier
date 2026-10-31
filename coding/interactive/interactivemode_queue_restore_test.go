package interactive

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

// queueingSession models the real session's queue: steering and follow-up
// messages land in the same lists ClearQueue drains.
type queueingSession struct{ *queueTestSession }

func (s queueingSession) Steer(message ai.Message) {
	s.queueTestSession.Steer(message)
	if user, ok := message.(*ai.UserMessage); ok {
		s.steering = append(s.steering, user.Content.Text)
	}
}

func (s queueingSession) FollowUp(message ai.Message) {
	s.queueTestSession.FollowUp(message)
	if user, ok := message.(*ai.UserMessage); ok {
		s.followUp = append(s.followUp, user.Content.Text)
	}
}

// pi v1.4.0 (4a080252) interactive-mode.ts flushCompactionQueue: followUp()
// pushes into the session queue synchronously, the prompt failure's catch
// clears the session queue, and only then does the pending rest list resume.
// The restored messages therefore live in the compaction queue exactly once.
// The port restores first and queues afterwards, so the rest message survives
// in both places and would be shown and sent twice.
func TestCompactionQueueFailureDoesNotDuplicateQueuedMessages(t *testing.T) {
	controller, session, _ := newQueueTestController(t)
	queueing := queueingSession{session}
	controller.Session = queueing
	controller.ShowError = func(string) {}

	controller.QueueCompactionMessage("first", "followUp")
	controller.QueueCompactionMessage("second", "followUp")
	session.promptErr = errors.New("boom")
	controller.FlushCompactionQueue(context.Background(), false)

	_, followUp := controller.GetAllQueuedMessages()
	occurrences := 0
	for _, message := range followUp {
		if message == "second" {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Fatalf("restored follow-up messages = %v, want the failed turn's rest message once", followUp)
	}
}
