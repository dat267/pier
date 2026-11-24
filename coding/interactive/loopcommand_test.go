package interactive

import (
	"strings"
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

// An empty prompt and an out-of-range interval are reported, not ignored.
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

	// 30s and 90m are rounded now (TestLoopCommandSaysWhenItRounds); past 28
	// days no cron day step can express the interval.
	failure = ""
	wiring.HandleLoopCommand("29d too slow")
	if failure == "" {
		t.Fatal("a 29-day interval was accepted")
	}
	if jobs := scheduler.Jobs(); len(jobs) != 0 {
		t.Fatalf("a refused /loop scheduled %d jobs", len(jobs))
	}
}

// A disabled loop feature says so, for the self-paced form too, instead of
// running a prompt the user turned off.
func TestLoopCommandReportsDisabledFeature(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	var failure string
	var submitted []string
	wiring := &CommandWiring{
		LoopScheduler:      scheduler,
		OnLoopPrompt:       func(prompt string) { submitted = append(submitted, prompt) },
		LoopFeatureEnabled: func() bool { return false },
		ShowStatus:         func(string) {},
		ShowError:          func(message string) { failure = message },
	}
	for _, args := range []string{"5m check the deploy", "check the deploy"} {
		failure, submitted = "", nil
		wiring.HandleLoopCommand(args)
		if failure == "" {
			t.Fatalf("a disabled feature accepted %q", args)
		}
		if len(submitted) != 0 {
			t.Fatalf("a disabled feature ran %v", submitted)
		}
		if jobs := scheduler.Jobs(); len(jobs) != 0 {
			t.Fatalf("a disabled feature scheduled %d jobs", len(jobs))
		}
	}
}

// D-row loop: the user can see and cancel what /loop scheduled without asking
// the agent. `/loop list` names every job, `/loop stop <id>` cancels one and
// `/loop stop` cancels all; a prompt that only starts with those words is still
// a prompt.
func TestLoopCommandListsAndStopsJobs(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := coding.NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	var status, failure string
	var submitted []string
	wiring := &CommandWiring{
		LoopScheduler: scheduler,
		OnLoopPrompt:  func(prompt string) { submitted = append(submitted, prompt) },
		ShowStatus:    func(message string) { status = message },
		ShowError:     func(message string) { failure = message },
	}

	wiring.HandleLoopCommand("list")
	if status != "No loops scheduled." {
		t.Fatalf("empty list status = %q", status)
	}
	wiring.HandleLoopCommand("5m check the deploy")
	wiring.HandleLoopCommand("1h summarize the logs")
	jobs := scheduler.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	wiring.HandleLoopCommand("list")
	for _, want := range []string{jobs[0].ID, "check the deploy", jobs[0].HumanSchedule, jobs[1].ID, "summarize the logs"} {
		if !strings.Contains(status, want) {
			t.Fatalf("list lacks %q:\n%s", want, status)
		}
	}

	wiring.HandleLoopCommand("stop nope")
	if failure == "" || len(scheduler.Jobs()) != 2 {
		t.Fatalf("an unknown id must be reported and cancel nothing (error %q)", failure)
	}
	failure = ""
	wiring.HandleLoopCommand("stop " + jobs[0].ID)
	if left := scheduler.Jobs(); len(left) != 1 || left[0].ID != jobs[1].ID {
		t.Fatalf("stop %s left %+v", jobs[0].ID, left)
	}
	wiring.HandleLoopCommand("stop")
	if left := scheduler.Jobs(); len(left) != 0 || !strings.Contains(status, "1") {
		t.Fatalf("stop all left %d jobs, status %q", len(left), status)
	}

	wiring.HandleLoopCommand("list the open files")
	if len(submitted) != 1 || submitted[0] != "list the open files" || failure != "" {
		t.Fatalf("a prompt starting with list must run as a prompt: %v (error %q)", submitted, failure)
	}
}

// D537: an interval with uneven cron gaps is rounded, and the status says so,
// so `/loop 45m` does not quietly fire at :00 and :45.
func TestLoopCommandSaysWhenItRounds(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	var status string
	wiring := &CommandWiring{
		LoopScheduler: scheduler,
		ShowStatus:    func(message string) { status = message },
		ShowError:     func(message string) { t.Fatalf("unexpected error: %s", message) },
	}
	wiring.HandleLoopCommand("7m check the deploy")
	if !strings.Contains(status, "Every 6 minutes") || !strings.Contains(status, "7m") {
		t.Fatalf("a rounded interval must say what it became and from what: %q", status)
	}
	for input, want := range map[string]string{"30s fast": "; 30s rounded", "90m slow": "; 1h30m rounded"} {
		wiring.HandleLoopCommand(input)
		if !strings.Contains(status, want) {
			t.Fatalf("%q: status %q lacks %q", input, status, want)
		}
	}
	wiring.HandleLoopCommand("5m check again")
	if strings.Contains(status, "rounded") {
		t.Fatalf("an even interval must not claim it was rounded: %q", status)
	}
}
