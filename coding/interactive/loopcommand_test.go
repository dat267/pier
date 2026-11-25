package interactive

import (
	"slices"
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
	// The model is told it is in a self-paced loop (D537), or it runs once.
	if len(submitted) != 1 || submitted[0] != coding.LoopSelfPacedPrompt("check the deploy") {
		t.Fatalf("submitted = %q, want the noted prompt run once", submitted)
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
	if len(submitted) != 1 || submitted[0] != coding.LoopSelfPacedPrompt("list the open files") || failure != "" {
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

// D537: a run on another day names its day. The list printed only the clock, so
// a daily loop created at 10:00 read "next 00:00", which looks like a run in the
// past, and in 24-hour time beside a schedule written "12:00 AM".
func TestLoopListNamesTheDayOfALaterRun(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.Local)
	scheduler := coding.NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	var status string
	wiring := &CommandWiring{LoopScheduler: scheduler, OnLoopPrompt: func(string) {},
		ShowStatus: func(message string) { status = message }, ShowError: func(string) {}}
	wiring.HandleLoopCommand("5m check the deploy")
	wiring.HandleLoopCommand("1d water the plants")
	wiring.HandleLoopCommand("7d send the report")
	// A whole-day loop steps by calendar days, not by cron's day-of-month step.
	for _, job := range scheduler.Jobs() {
		if want := map[string]int{"water the plants": 1, "send the report": 7}[job.Prompt]; job.EveryDays != want {
			t.Fatalf("%q steps by %d days, want %d", job.Prompt, job.EveryDays, want)
		}
	}
	// A job the agent scheduled with cron can run days away.
	if _, err := scheduler.Add("0 9 1 * *", "pay the rent", true, 0); err != nil {
		t.Fatal(err)
	}
	wiring.HandleLoopCommand("list")
	for _, want := range []string{"next 10:05 AM  check the deploy", "next tomorrow 12:00 AM  water the plants", "next tomorrow 12:00 AM  send the report", "next Tue Dec 1 9:00 AM  pay the rent"} {
		if !strings.Contains(status, want) {
			t.Fatalf("list lacks %q:\n%s", want, status)
		}
	}
}

// D537: a job the agent scheduled can carry a multi-line prompt. Each job is one
// row of /loop list, so the row shows the prompt's first line and marks the rest,
// instead of spilling the prompt's later lines under it as if they were rows.
func TestLoopListKeepsEachJobOnOneRow(t *testing.T) {
	scheduler := coding.NewLoopScheduler(func() time.Time { return time.Date(2026, time.November, 25, 10, 0, 0, 0, time.Local) }, nil)
	defer scheduler.Stop()
	var status string
	wiring := &CommandWiring{LoopScheduler: scheduler, OnLoopPrompt: func(string) {},
		ShowStatus: func(message string) { status = message }, ShowError: func(string) {}}
	if _, err := scheduler.Add("*/5 * * * *", "Check the deploy.\n\n1. Read the logs\n2. Report failures", true, 0); err != nil {
		t.Fatal(err)
	}
	wiring.HandleLoopCommand("5m tidy up")
	// A self-paced wakeup lists its prompt, not the note the model reads.
	if _, err := scheduler.SetWakeup(time.Minute, coding.LoopSelfPacedPrompt("watch the queue")); err != nil {
		t.Fatal(err)
	}
	wiring.HandleLoopCommand("list")
	lines := strings.Split(status, "\n")
	if !strings.HasSuffix(lines[len(lines)-1], "  watch the queue") {
		t.Fatalf("the wakeup's row = %q", lines[len(lines)-1])
	}
	lines = lines[:len(lines)-1]
	if len(lines) != 3 {
		t.Fatalf("two jobs took %d lines:\n%s", len(lines), status)
	}
	if !strings.HasSuffix(lines[1], "Check the deploy. …") {
		t.Fatalf("the multi-line job's row = %q", lines[1])
	}
}

// D537: a self-paced /loop typed during a run does not go into the message
// queue. Its first run waits in the scheduler, as a wakeup due now that fires
// once the agent is idle, and the status says so.
func TestLoopCommandSaysWhenTheFirstRunWaits(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	session := &commandTestSession{}
	var status string
	var submitted []string
	wiring := &CommandWiring{LoopScheduler: scheduler, Session: session,
		OnLoopPrompt: func(prompt string) { submitted = append(submitted, prompt) },
		ShowStatus:   func(message string) { status = message }, ShowError: func(string) {}}
	wiring.HandleLoopCommand("check the deploy")
	if !strings.HasPrefix(status, "Loop started.") || len(submitted) != 1 {
		t.Fatalf("an idle start said %q and submitted %q", status, submitted)
	}
	for _, busy := range []func(){func() { session.streaming = true }, func() { session.compacting = true }} {
		session.streaming, session.compacting, submitted = false, false, nil
		scheduler.RemoveAll()
		busy()
		wiring.HandleLoopCommand("check the deploy")
		if len(submitted) != 0 {
			t.Fatalf("a busy start submitted %q into the message queue", submitted)
		}
		jobs := scheduler.Jobs()
		if len(jobs) != 1 || jobs[0].Recurring || jobs[0].Prompt != coding.LoopSelfPacedPrompt("check the deploy") {
			t.Fatalf("a busy start scheduled %+v, want one wakeup for the noted prompt", jobs)
		}
		if !strings.Contains(status, "once the agent is idle") {
			t.Fatalf("a busy start said %q", status)
		}
		// Its /loop list row says when it runs, not "Once, in 0 seconds".
		wiring.HandleLoopCommand("list")
		// No clock either: it runs when the agent is idle, not at a time.
		if !strings.Contains(status, "Once, when the agent is idle  check the deploy") || strings.Contains(status, "0 seconds") {
			t.Fatalf("a busy start listed as %q", status)
		}
	}
}

// D537: a fire never enters the message queue while the agent works. A
// recurring job's fire is skipped (its next interval runs as usual); a one-shot,
// such as a self-paced loop's wakeup, is retried shortly, since skipping it would
// end the loop; an idle agent gets the prompt at once.
func TestLoopFireWaitsForAnIdleAgent(t *testing.T) {
	recurring := coding.LoopJob{Prompt: "poll", Recurring: true}
	once := coding.LoopJob{Prompt: "resume the loop"}
	for _, tc := range []struct {
		job       coding.LoopJob
		busy      bool
		submitted []string
		retried   []string
	}{
		{recurring, false, []string{"poll"}, nil},
		{once, false, []string{"resume the loop"}, nil},
		{recurring, true, nil, nil},
		{once, true, nil, []string{"resume the loop"}},
	} {
		var submitted, retried []string
		deliverLoopFire(tc.job, tc.busy,
			func(prompt string) { submitted = append(submitted, prompt) },
			func(job coding.LoopJob) { retried = append(retried, job.Prompt) })
		if !slices.Equal(submitted, tc.submitted) || !slices.Equal(retried, tc.retried) {
			t.Fatalf("job %+v busy %v: submitted %q retried %q, want %q and %q",
				tc.job, tc.busy, submitted, retried, tc.submitted, tc.retried)
		}
	}
}

// D537: /loop stop with no id names what it stopped in plain words: one loop
// is "1 loop", and nothing scheduled says so instead of "Stopped 0 loop(s)".
func TestLoopStopAllCountsInWords(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	var status string
	wiring := &CommandWiring{LoopScheduler: scheduler,
		ShowStatus: func(message string) { status = message }, ShowError: func(message string) { status = message }}
	for _, tc := range []struct {
		jobs int
		want string
	}{{0, "No loops scheduled."}, {1, "Stopped 1 loop."}, {2, "Stopped 2 loops."}} {
		for range tc.jobs {
			if _, err := scheduler.Add("*/5 * * * *", "poll", true, 0); err != nil {
				t.Fatal(err)
			}
		}
		wiring.HandleLoopCommand("stop")
		if status != tc.want || scheduler.Count() != 0 {
			t.Fatalf("%d job(s): /loop stop said %q, left %d", tc.jobs, status, scheduler.Count())
		}
	}
}

// D537: /loop stop takes several ids. "/loop stop 1 2" used to fall through to
// the prompt path and start a self-paced loop told to "stop 1 2"; an unknown id
// among them is named without undoing the ones that were stopped.
func TestLoopStopTakesSeveralIds(t *testing.T) {
	scheduler := coding.NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	for range 3 {
		if _, err := scheduler.Add("*/5 * * * *", "poll", true, 0); err != nil {
			t.Fatal(err)
		}
	}
	var status, errText string
	var submitted []string
	wiring := &CommandWiring{LoopScheduler: scheduler, Session: &commandTestSession{},
		OnLoopPrompt: func(prompt string) { submitted = append(submitted, prompt) },
		ShowStatus:   func(message string) { status = message }, ShowError: func(message string) { errText = message }}
	wiring.HandleLoopCommand("stop 1 2")
	if len(submitted) != 0 {
		t.Fatalf("/loop stop 1 2 started a loop: %q", submitted)
	}
	if status != "Loops 1 and 2 stopped." || scheduler.Count() != 1 {
		t.Fatalf("status %q, %d left", status, scheduler.Count())
	}
	// A prompt that starts with "stop" names no job, so it still runs.
	wiring.HandleLoopCommand("stop the deploy checks")
	if len(submitted) != 1 || scheduler.Count() != 1 {
		t.Fatalf("a stop-shaped prompt submitted %q, %d left", submitted, scheduler.Count())
	}
	wiring.HandleLoopCommand("stop 3 9")
	if scheduler.Count() != 0 || errText != "No loop 9. /loop list shows the scheduled ones." || status != "Loop 3 stopped." {
		t.Fatalf("status %q, error %q, %d left", status, errText, scheduler.Count())
	}
}
