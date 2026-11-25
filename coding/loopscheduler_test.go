package coding

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// `/loop` accepts a leading interval token, a trailing "every" clause, or
// neither, in which case the whole input is the prompt and the loop self-paces.
func TestParseLoopInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		interval time.Duration
		prompt   string
	}{
		{"leading minutes", "5m /babysit-prs", 5 * time.Minute, "/babysit-prs"},
		{"leading hours", "2h check the deploy", 2 * time.Hour, "check the deploy"},
		{"leading seconds", "30s run the tests", 30 * time.Second, "run the tests"},
		{"leading days", "1d rotate the logs", 24 * time.Hour, "rotate the logs"},
		{"trailing short", "check the deploy every 20m", 20 * time.Minute, "check the deploy"},
		{"trailing word", "run tests every 5 minutes", 5 * time.Minute, "run tests"},
		{"trailing singular", "run tests every 1 minute", time.Minute, "run tests"},
		{"trailing hours", "sync every 2 hours", 2 * time.Hour, "sync"},
		{"no interval", "check the deploy", 0, "check the deploy"},
		{"every is not a time", "check every PR", 0, "check every PR"},
		{"every with bad unit", "check every 5 parsecs", 0, "check every 5 parsecs"},
		{"interval alone is the prompt", "5m", 0, "5m"},
		{"leading interval wins", "5m check every 20m", 5 * time.Minute, "check every 20m"},
		{"extra spacing", "  5m   check  the  deploy  ", 5 * time.Minute, "check  the  deploy"},
		{"empty", "", 0, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interval, prompt := ParseLoopInput(test.input)
			if interval != test.interval {
				t.Fatalf("interval = %v, want %v", interval, test.interval)
			}
			if strings.TrimSpace(prompt) != test.prompt {
				t.Fatalf("prompt = %q, want %q", prompt, test.prompt)
			}
		})
	}
}

// An interval becomes the cron expression the scheduler arms.
func TestLoopCronForInterval(t *testing.T) {
	// D537: an interval snaps to the nearest step that divides its unit, so the
	// gaps are even. */45 fired at :00 and :45 (gaps of 45 and 15 minutes) and
	// */7 left a 4-minute gap before :00; 90s became every minute while 30s was
	// refused. Ties go to the longer step: fewer runs is the safer mistake.
	tests := []struct {
		interval time.Duration
		want     string
		used     time.Duration
		ok       bool
	}{
		{time.Minute, "*/1 * * * *", time.Minute, true},
		{5 * time.Minute, "*/5 * * * *", 5 * time.Minute, true},
		{7 * time.Minute, "*/6 * * * *", 6 * time.Minute, true},
		{45 * time.Minute, "0 */1 * * *", time.Hour, true},
		{40 * time.Minute, "*/30 * * * *", 30 * time.Minute, true},
		{59 * time.Minute, "0 */1 * * *", time.Hour, true},
		{time.Hour, "0 */1 * * *", time.Hour, true},
		{90 * time.Minute, "0 */2 * * *", 2 * time.Hour, true},
		{6 * time.Hour, "0 */6 * * *", 6 * time.Hour, true},
		{5 * time.Hour, "0 */6 * * *", 6 * time.Hour, true},
		{23 * time.Hour, "0 0 * * *", 24 * time.Hour, true},
		{24 * time.Hour, "0 0 * * *", 24 * time.Hour, true},
		{25 * time.Hour, "0 0 * * *", 24 * time.Hour, true},
		{48 * time.Hour, "0 0 */2 * *", 48 * time.Hour, true},
		// Seconds round up to a whole minute rather than down to none.
		{30 * time.Second, "*/1 * * * *", time.Minute, true},
		{90 * time.Second, "*/2 * * * *", 2 * time.Minute, true},
		{0, "", 0, false},
		{29 * 24 * time.Hour, "", 0, false},
	}
	for _, test := range tests {
		got, used, ok := LoopCronForInterval(test.interval)
		if ok != test.ok || got != test.want || used != test.used {
			t.Fatalf("LoopCronForInterval(%v) = %q, %v, %v; want %q, %v, %v",
				test.interval, got, used, ok, test.want, test.used, test.ok)
		}
	}
}

// A scheduled job fires once its minute arrives, and a one-shot job deletes
// itself after firing.
func TestLoopSchedulerFiresAndDeletes(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	clock := now
	var fired []string
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(prompt string) {
		fired = append(fired, prompt)
	})
	defer scheduler.Stop()

	job, err := scheduler.Add("* * * * *", "tick", false, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if job.HumanSchedule != "Every minute" {
		t.Fatalf("human schedule = %q, want the plain phrase", job.HumanSchedule)
	}
	if got := scheduler.Jobs(); len(got) != 1 {
		t.Fatalf("jobs = %d, want 1", len(got))
	}

	// Not yet due: the next match is the following minute.
	scheduler.fireDue()
	if len(fired) != 0 {
		t.Fatalf("fired %v before the minute arrived", fired)
	}

	clock = now.Add(time.Minute)
	scheduler.fireDue()
	if len(fired) != 1 || fired[0] != "tick" {
		t.Fatalf("fired = %v, want one tick", fired)
	}
	if got := scheduler.Jobs(); len(got) != 0 {
		t.Fatalf("one-shot job survived its fire: %d jobs", len(got))
	}
}

// A recurring job stays scheduled and fires again on the next match.
func TestLoopSchedulerRecurringStays(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	clock := now
	var fired []string
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(prompt string) {
		fired = append(fired, prompt)
	})
	defer scheduler.Stop()

	if _, err := scheduler.Add("*/5 * * * *", "poll", true, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, at := range []time.Time{now.Add(5 * time.Minute), now.Add(10 * time.Minute)} {
		clock = at
		scheduler.fireDue()
	}
	if len(fired) != 2 {
		t.Fatalf("fired = %v, want two", fired)
	}
	if got := scheduler.Jobs(); len(got) != 1 {
		t.Fatalf("recurring job left %d jobs, want 1", len(got))
	}
}

// Remove cancels a job, and an unknown id reports false.
func TestLoopSchedulerRemove(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	job, err := scheduler.Add("0 9 * * *", "morning", true, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !scheduler.Remove(job.ID) {
		t.Fatal("Remove reported the job missing")
	}
	if scheduler.Remove(job.ID) {
		t.Fatal("Remove reported a removed job as present")
	}
	if got := scheduler.Jobs(); len(got) != 0 {
		t.Fatalf("jobs = %d, want 0", len(got))
	}
}

// A refused schedule is an error rather than a silent no-op.
func TestLoopSchedulerRejectsBadSchedules(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()

	if _, err := scheduler.Add("* * * *", "x", true, 0); err == nil {
		t.Fatal("a four-field cron was accepted")
	}
	if _, err := scheduler.Add("0 0 31 2 *", "x", true, 0); err == nil {
		t.Fatal("a 31 February schedule was accepted")
	}
	if _, err := scheduler.Add("* * * * *", "x", true, 0); err != nil {
		t.Fatalf("a valid schedule was refused: %v", err)
	}
}

// The cap refuses a job past the limit instead of growing without bound.
func TestLoopSchedulerCap(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	for i := 0; i < loopMaxJobs; i++ {
		if _, err := scheduler.Add("0 9 * * *", "x", true, 0); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if _, err := scheduler.Add("0 9 * * *", "x", true, 0); err == nil {
		t.Fatalf("the %dth job was accepted past a cap of %d", loopMaxJobs+1, loopMaxJobs)
	}
}

// Stop drops the jobs, so a closed session holds nothing.
func TestLoopSchedulerStopDropsJobs(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	scheduler.Start()
	if _, err := scheduler.Add("0 9 * * *", "x", true, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	scheduler.Stop()
	if got := scheduler.Jobs(); len(got) != 0 {
		t.Fatalf("jobs after Stop = %d, want 0", len(got))
	}
	if _, err := scheduler.Add("0 9 * * *", "x", true, 0); err == nil {
		t.Fatal("Add after Stop was accepted")
	}
}

// A job's maximum age ends a recurring schedule.
func TestLoopSchedulerMaxAge(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	clock := now
	var fired int
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(string) { fired++ })
	defer scheduler.Stop()
	if _, err := scheduler.Add("* * * * *", "tick", true, time.Minute); err != nil {
		t.Fatalf("Add: %v", err)
	}
	clock = now.Add(time.Minute)
	scheduler.fireDue()
	if fired != 1 {
		t.Fatalf("fired %d times, want 1", fired)
	}
	// Past the maximum age the job is gone rather than rescheduled forever.
	clock = now.Add(2 * time.Minute)
	scheduler.fireDue()
	if got := scheduler.Jobs(); len(got) != 0 {
		t.Fatalf("jobs = %d after the maximum age, want 0", len(got))
	}
}

// A job is in-memory only: adding one writes nothing, and a fresh scheduler
// (what the next process gets) knows nothing about it. The only trace a loop
// leaves after the TUI closes is the prompts it fired, which are ordinary input
// and so part of the transcript.
func TestLoopSchedulerPersistsNothing(t *testing.T) {
	manager, dir := newTestSession(t)
	scheduler := NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model:         &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:         []agent.AgentTool{testTool("read")},
		LoopScheduler: scheduler,
		LoopMaxAge:    LoopRecurringMaxAge,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	before := sessionFileContents(t, manager)
	if _, err := scheduler.Add("*/5 * * * *", "poll the deploy", true, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if after := sessionFileContents(t, manager); after != before {
		t.Fatalf("scheduling changed the session file:\n before %q\n after  %q", before, after)
	}
	// What the next process starts with.
	fresh := NewLoopScheduler(nil, nil)
	if jobs := fresh.Jobs(); len(jobs) != 0 {
		fresh.Stop()
		t.Fatalf("a new scheduler found %d jobs, so jobs outlive the process", len(jobs))
	}
	fresh.Stop()

	// A fired prompt is the one durable trace: it goes through ordinary input,
	// so the transcript keeps it like a typed message.
	if err := session.PromptText(t.Context(), "poll the deploy"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := sessionFileContents(t, manager); !strings.Contains(got, "poll the deploy") {
		t.Fatalf("a fired prompt did not reach the transcript:\n%s", got)
	}
}

// sessionFileContents reads the session file, or "" when none exists yet.
func sessionFileContents(t *testing.T, manager *SessionManager) string {
	t.Helper()
	path := manager.GetSessionFile()
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read session file: %v", err)
	}
	return string(raw)
}

// D537: a day step stays even across a month end. Cron's day-of-month step
// restarts on the 1st, so "*/7" ran Nov 29 then Dec 1 and "*/3" ran Dec 31 then
// Jan 1; AddEveryDays advances by calendar days instead.
func TestLoopSchedulerDayStepCrossesMonthEnd(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.Local)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(string) {})
	defer scheduler.Stop()
	for _, days := range []int{3, 7} {
		scheduler.RemoveAll()
		job, err := scheduler.AddEveryDays(days, "water the plants", 0)
		if err != nil {
			t.Fatalf("AddEveryDays(%d): %v", days, err)
		}
		if job.HumanSchedule != "Every "+strconv.Itoa(days)+" days at 12:00 AM" {
			t.Fatalf("schedule = %q", job.HumanSchedule)
		}
		var runs []time.Time
		for range 5 {
			next := scheduler.Jobs()[0].NextFire
			runs = append(runs, next)
			clock = next
			scheduler.fireDue()
		}
		clock = time.Date(2026, time.November, 25, 10, 0, 0, 0, time.Local)
		if first := runs[0]; first != time.Date(2026, time.November, 26, 0, 0, 0, 0, time.Local) {
			t.Fatalf("%d days: the first run is %v, want the next midnight", days, first)
		}
		for index := 1; index < len(runs); index++ {
			if want := runs[index-1].AddDate(0, 0, days); runs[index] != want {
				t.Fatalf("%d days: runs %v, gap %d ends at %v, want %v", days, runs, index, runs[index], want)
			}
		}
	}
}

// D537: a fire the host could not deliver because the agent was busy goes back
// under its own id. It does not replace the self-paced wakeup the way SetWakeup
// would, a job already present is not duplicated, and a postponed wakeup is
// still the one schedule_wakeup's stop clears.
func TestLoopSchedulerPostpone(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	var fired []LoopJob
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	scheduler.SetFire(func(job LoopJob) { fired = append(fired, job) })
	once, err := scheduler.Add("5 10 * * *", "one-shot from cron_create", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	wakeup, err := scheduler.SetWakeup(10*time.Minute, "the loop's wakeup")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Minute)
	scheduler.fireDue()
	if len(fired) != 1 || fired[0].ID != once.ID || fired[0].Recurring {
		t.Fatalf("fired %+v, want the one-shot job", fired)
	}
	if !scheduler.Postpone(fired[0], 5*time.Second) {
		t.Fatal("Postpone refused the fired job")
	}
	if scheduler.Postpone(fired[0], 5*time.Second) {
		t.Fatal("Postpone duplicated a job that is already back")
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 2 || jobs[0].ID != wakeup.ID || jobs[1].ID != once.ID || !jobs[1].NextFire.Equal(clock.Add(5*time.Second)) {
		t.Fatalf("jobs after postponing = %+v", jobs)
	}
	clock = clock.Add(5 * time.Second)
	scheduler.fireDue()
	if len(fired) != 2 || fired[1].ID != once.ID {
		t.Fatalf("the postponed job did not fire again: %+v", fired)
	}
	// A postponed wakeup keeps its id, so stop still clears it.
	clock = clock.Add(10 * time.Minute)
	scheduler.fireDue()
	if !scheduler.Postpone(fired[len(fired)-1], 5*time.Second) || !scheduler.ClearWakeup() || scheduler.Count() != 0 {
		t.Fatalf("a postponed wakeup was not cleared by stop: %+v", scheduler.Jobs())
	}
}
