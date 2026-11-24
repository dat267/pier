package interactive

import (
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// /loop with an interval arms a cron job on the session's scheduler, and the
// status names the schedule.
func TestLoopCommandArmsCronJob(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := coding.NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	var status, failure string
	wiring := &CommandWiring{
		LoopScheduler: scheduler,
		ShowStatus:    func(message string) { status = message },
		ShowError:     func(message string) { failure = message },
	}

	wiring.HandleLoopCommand("5m check the deploy")
	if failure != "" {
		t.Fatalf("unexpected error: %s", failure)
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	if jobs[0].Prompt != "check the deploy" || jobs[0].Cron != "*/5 * * * *" {
		t.Fatalf("job = %+v", jobs[0])
	}
	if status == "" {
		t.Fatal("no status was shown")
	}
}

// Without an interval /loop runs the prompt now instead of scheduling it.
func TestLoopCommandSelfPacedRunsNow(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	var submitted []string
	wiring := &CommandWiring{
		LoopScheduler: scheduler,
		OnLoopPrompt:  func(prompt string) { submitted = append(submitted, prompt) },
		ShowStatus:    func(string) {},
		ShowError:     func(message string) { t.Fatalf("unexpected error: %s", message) },
	}
	wiring.HandleLoopCommand("check the deploy")
	if len(submitted) != 1 || submitted[0] != "check the deploy" {
		t.Fatalf("submitted = %v, want the prompt run once", submitted)
	}
	// A self-paced loop arms no cron job of its own.
	if jobs := scheduler.Jobs(); len(jobs) != 0 {
		t.Fatalf("jobs = %d, want none", len(jobs))
	}
}

// An empty prompt and an unexpressible interval are reported, not ignored.
func TestLoopCommandRejectsBadInput(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	var failure string
	wiring := &CommandWiring{
		LoopScheduler: scheduler,
		ShowStatus:    func(string) {},
		ShowError:     func(message string) { failure = message },
	}

	failure = ""
	wiring.HandleLoopCommand("")
	if failure == "" {
		t.Fatal("an empty /loop was accepted")
	}

	failure = ""
	wiring.HandleLoopCommand("30s too fast")
	if failure == "" {
		t.Fatal("a sub-minute interval was accepted")
	}

	failure = ""
	wiring.HandleLoopCommand("90m awkward")
	if failure == "" {
		t.Fatal("a 90-minute interval was accepted")
	}
	if jobs := scheduler.Jobs(); len(jobs) != 0 {
		t.Fatalf("a refused /loop scheduled %d jobs", len(jobs))
	}
}
