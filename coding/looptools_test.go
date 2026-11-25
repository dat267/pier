package coding

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// callLoopTool runs a tool from LoopTools by name and returns its text and error
// flag.
func callLoopTool(t *testing.T, tools []agent.AgentTool, name string, params string) (string, bool) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name != name {
			continue
		}
		result, err := tool.Execute(context.Background(), "call", json.RawMessage(params), nil)
		if err != nil {
			t.Fatalf("%s returned an error: %v", name, err)
		}
		var text strings.Builder
		for _, block := range result.Content {
			if value, ok := block.(ai.TextContent); ok {
				text.WriteString(value.Text)
			}
		}
		return text.String(), result.IsError
	}
	t.Fatalf("tool %q is not registered", name)
	return "", false
}

// A cron_create call schedules a job the scheduler reports, and cron_delete
// cancels it.
func TestCronToolsCreateAndDelete(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler, MaxAge: 7 * 24 * time.Hour})

	text, isError := callLoopTool(t, tools, "cron_create", `{"cron":"*/5 * * * *","prompt":"poll the deploy"}`)
	if isError {
		t.Fatalf("cron_create failed: %s", text)
	}
	if !strings.Contains(text, "Every 5 minutes") {
		t.Fatalf("result did not report the schedule: %q", text)
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 1 || jobs[0].Prompt != "poll the deploy" || !jobs[0].Recurring {
		t.Fatalf("jobs = %+v, want one recurring job", jobs)
	}

	if _, isError := callLoopTool(t, tools, "cron_delete", `{"id":"`+jobs[0].ID+`"}`); isError {
		t.Fatal("cron_delete failed")
	}
	if got := scheduler.Jobs(); len(got) != 0 {
		t.Fatalf("jobs = %d after delete, want 0", len(got))
	}
}

// A refused schedule is reported as a tool error with the reason, not a panic.
func TestCronCreateRejectsBadInput(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler})

	for _, params := range []string{
		`{"cron":"* * * *","prompt":"x"}`,
		`{"cron":"0 0 31 2 *","prompt":"x"}`,
		`{"cron":"* * * * *","prompt":"   "}`,
	} {
		text, isError := callLoopTool(t, tools, "cron_create", params)
		if !isError {
			t.Fatalf("cron_create accepted %s: %q", params, text)
		}
	}
}

// A one-shot create reports that it fires once, and cron_list reports the jobs.
func TestCronCreateOneShotAndList(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	scheduler := NewLoopScheduler(func() time.Time { return clock }, nil)
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler})

	text, isError := callLoopTool(t, tools, "cron_create",
		`{"cron":"30 14 * * *","prompt":"remind me","recurring":false}`)
	if isError {
		t.Fatalf("cron_create failed: %s", text)
	}
	if !strings.Contains(text, "one-shot") {
		t.Fatalf("result did not say one-shot: %q", text)
	}
	listed, isError := callLoopTool(t, tools, "cron_list", `{}`)
	if isError || !strings.Contains(listed, "remind me") {
		t.Fatalf("cron_list = %q (error %v)", listed, isError)
	}
}

// An empty session lists no jobs rather than an empty line.
func TestCronListEmpty(t *testing.T) {
	scheduler := NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler})
	text, isError := callLoopTool(t, tools, "cron_list", `{}`)
	if isError || text != "No scheduled jobs." {
		t.Fatalf("cron_list = %q (error %v)", text, isError)
	}
}

// The self-paced wakeup clamps its delay and refuses a missing prompt.
func TestScheduleWakeupClampsDelay(t *testing.T) {
	var got time.Duration
	var prompt string
	var stopped bool
	tools := LoopTools(LoopToolOptions{
		OnWakeup: func(delay time.Duration, p string) { got, prompt = delay, p },
		OnStop:   func() { stopped = true },
	})

	// Below the floor.
	if _, isError := callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":5,"prompt":"/loop check"}`); isError {
		t.Fatal("a short delay was refused")
	}
	if got != loopWakeupMinDelay {
		t.Fatalf("delay = %v, want the floor %v", got, loopWakeupMinDelay)
	}
	if prompt != "/loop check" {
		t.Fatalf("prompt = %q", prompt)
	}

	// Above the ceiling.
	callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":99999,"prompt":"/loop check"}`)
	if got != loopWakeupMaxDelay {
		t.Fatalf("delay = %v, want the ceiling %v", got, loopWakeupMaxDelay)
	}

	// A wakeup with no prompt is refused.
	if _, isError := callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":120}`); !isError {
		t.Fatal("a wakeup with no prompt was accepted")
	}

	// stop ends the loop and does not arm a wakeup.
	before := got
	if text, isError := callLoopTool(t, tools, "schedule_wakeup", `{"stop":true}`); isError {
		t.Fatalf("stop failed: %s", text)
	}
	if !stopped {
		t.Fatal("stop did not reach the host")
	}
	if got != before {
		t.Fatalf("stop armed a wakeup of %v", got)
	}
}

// D537: a self-paced loop's schedule_wakeup arms a real one-shot job. Nothing
// wired OnWakeup, so the tool said "The loop resumes in 2 minutes" and armed
// nothing; and its stop called RemoveAll, wiping the user's /loop 5m jobs too.
// One wakeup is pending at a time, and stop clears only it.
func TestScheduleWakeupArmsTheScheduler(t *testing.T) {
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	var fired []string
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(prompt string) { fired = append(fired, prompt) })
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler, MaxAge: time.Hour})
	if _, isError := callLoopTool(t, tools, "cron_create", `{"cron":"*/5 * * * *","prompt":"poll the deploy"}`); isError {
		t.Fatal("cron_create failed")
	}

	if text, isError := callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":120,"prompt":"/loop check"}`); isError {
		t.Fatalf("schedule_wakeup failed: %s", text)
	}
	callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":180,"prompt":"/loop check"}`)
	if jobs := scheduler.Jobs(); len(jobs) != 2 {
		t.Fatalf("jobs = %d, want the cron job and one wakeup", len(jobs))
	}
	clock = clock.Add(170 * time.Second)
	scheduler.fireDue()
	if len(fired) != 0 {
		t.Fatalf("the replaced 120s wakeup fired: %v", fired)
	}
	clock = clock.Add(20 * time.Second)
	scheduler.fireDue()
	if len(fired) != 1 || fired[0] != "/loop check" {
		t.Fatalf("fired = %v, want the wakeup's prompt once", fired)
	}

	callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":600,"prompt":"/loop check"}`)
	if _, isError := callLoopTool(t, tools, "schedule_wakeup", `{"stop":true}`); isError {
		t.Fatal("stop failed")
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 1 || jobs[0].Prompt != "poll the deploy" {
		t.Fatalf("stop must clear only the wakeup, left %+v", jobs)
	}
}

// D537: the model only sees a self-paced loop's prompt, so the prompt says it is
// a loop and how to repeat or end it. Without the note `/loop check the deploy`
// ran once and stopped: nothing told the model to call schedule_wakeup. A
// resumed run carries the note again, once, and a slash-command prompt is left
// for its own command to handle.
func TestSelfPacedLoopPromptCarriesTheNote(t *testing.T) {
	noted := LoopSelfPacedPrompt("check the deploy")
	if !strings.HasPrefix(noted, "check the deploy\n\n") || !strings.Contains(noted, "schedule_wakeup") || !strings.Contains(noted, "stop") {
		t.Fatalf("noted prompt = %q", noted)
	}
	if again := LoopSelfPacedPrompt(noted); again != noted {
		t.Fatalf("noting twice = %q", again)
	}
	if got := LoopPromptWithoutNote(noted); got != "check the deploy" {
		t.Fatalf("without the note = %q", got)
	}
	// Ordinary input, which has no note, keeps its exact text.
	if got := LoopPromptWithoutNote("  keep my spacing \n"); got != "  keep my spacing \n" {
		t.Fatalf("plain text came back as %q", got)
	}
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	var fired []string
	scheduler := NewLoopScheduler(func() time.Time { return clock }, func(prompt string) { fired = append(fired, prompt) })
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler, MaxAge: time.Hour})
	for _, prompt := range []string{"check the deploy", noted, "/loop check the deploy"} {
		fired = nil
		raw, _ := json.Marshal(prompt)
		if text, isError := callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":60,"prompt":`+string(raw)+`}`); isError {
			t.Fatalf("schedule_wakeup failed: %s", text)
		}
		clock = clock.Add(61 * time.Second)
		scheduler.fireDue()
		want := noted
		if strings.HasPrefix(prompt, "/") {
			want = prompt
		}
		if len(fired) != 1 || fired[0] != want {
			t.Fatalf("a wakeup for %q fired %q, want %q", prompt, fired, want)
		}
	}
}

// D537: cron_list is one row per job. A multi-line prompt spread one job over
// several lines (its "1. read the logs" read as another row), and a self-paced
// wakeup's row repeated the note meant for the run itself.
func TestCronListKeepsEachJobOnOneRow(t *testing.T) {
	scheduler := NewLoopScheduler(func() time.Time { return time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC) }, nil)
	defer scheduler.Stop()
	tools := LoopTools(LoopToolOptions{Scheduler: scheduler, MaxAge: time.Hour})
	if _, isError := callLoopTool(t, tools, "cron_create", `{"cron":"*/5 * * * *","prompt":"check the deploy\n\n1. read the logs"}`); isError {
		t.Fatal("cron_create failed")
	}
	if _, isError := callLoopTool(t, tools, "schedule_wakeup", `{"delay_seconds":60,"prompt":"watch the queue"}`); isError {
		t.Fatal("schedule_wakeup failed")
	}
	text, isError := callLoopTool(t, tools, "cron_list", `{}`)
	if isError {
		t.Fatalf("cron_list failed: %s", text)
	}
	rows := strings.Split(text, "\n")
	if len(rows) != 2 {
		t.Fatalf("two jobs listed on %d lines:\n%s", len(rows), text)
	}
	if !strings.HasSuffix(rows[0], ": check the deploy 1. read the logs") || !strings.HasSuffix(rows[1], ": watch the queue") {
		t.Fatalf("rows = %q", rows)
	}
}

// D537: the scheduling tools reach the system prompt the way the goal tools do
// (upstream system-prompt.ts lists only tools with a promptSnippet). Without an
// entry they were absent from the tools section while set_goal was listed with
// guidance, so the model reached for a goal when the user asked for repetition.
// The bullets split the two: repetition is a loop, one objective is a goal. Both
// install paths carry them: a session built with a scheduler, and the
// interactive app's AdoptLoopScheduler.
func TestLoopToolsContributeToSystemPrompt(t *testing.T) {
	scheduler := NewLoopScheduler(nil, nil)
	defer scheduler.Stop()
	adopted := NewLoopScheduler(nil, nil)
	defer adopted.Stop()
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model:         &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:         []agent.AgentTool{testTool("read")},
		LoopScheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	for _, step := range []string{"built", "adopted"} {
		if step == "adopted" {
			session.AdoptLoopScheduler(adopted, LoopRecurringMaxAge)
		}
		prompt := session.SystemPrompt()
		for _, want := range []string{
			"- cron_create: Schedule a prompt to run later, on a cron schedule or once",
			"- cron_list: List the prompts scheduled in this session",
			"- cron_delete: Cancel a scheduled prompt by its id",
			"- schedule_wakeup: Set the delay before the next run of a self-paced /loop, or end it",
			"Use cron_create, not set_goal, when the user wants something repeated on an interval or done at a set time",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s: system prompt is missing %q:\n%s", step, want, prompt)
			}
		}
	}
}
