package coding

// The tools a scheduled prompt is managed with: one that arms a self-paced
// wakeup, and two that create and delete cron-scheduled prompts.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// loopSelfPacedNote tells the model a prompt is one run of a self-paced loop
// (D537). It sees only the prompt, so without it a loop runs once: nothing says
// schedule_wakeup applies.
const loopSelfPacedNote = "(This is one run of a self-paced /loop. When it is done, call schedule_wakeup " +
	"with the delay before the next run and this prompt, or with stop: true once nothing is left to do.)"

// LoopSelfPacedPrompt appends the self-paced note to prompt, once.
func LoopSelfPacedPrompt(prompt string) string {
	return LoopPromptWithoutNote(prompt) + "\n\n" + loopSelfPacedNote
}

// LoopPromptWithoutNote is prompt as the user wrote it, without the note. Text
// that carries no note is returned unchanged, so ordinary input keeps its
// spacing.
func LoopPromptWithoutNote(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasSuffix(trimmed, loopSelfPacedNote) {
		return prompt
	}
	return strings.TrimSpace(strings.TrimSuffix(trimmed, loopSelfPacedNote))
}

// LoopWakeupDelayBounds clamp a self-paced wakeup. The floor keeps a loop from
// spinning, and the ceiling keeps it from sleeping past any cache window.
const (
	loopWakeupMinDelay = 60 * time.Second
	loopWakeupMaxDelay = 3600 * time.Second
)

// loopPromptSentinel resolves back to the autonomous-loop instructions at fire
// time, so a self-paced loop can re-arm itself without repeating them.
const loopPromptSentinel = "<<autonomous-loop-dynamic>>"

// LoopToolOptions configure the scheduling tools.
type LoopToolOptions struct {
	// Scheduler owns the jobs. A nil scheduler leaves the tools inert.
	Scheduler *LoopScheduler
	// MaxAge is a recurring job's lifetime; zero means never expire.
	MaxAge time.Duration
	// OnWakeup, when set, replaces arming the wakeup on Scheduler (tests).
	// Otherwise the scheduler holds one pending wakeup, replaced by each call.
	OnWakeup func(delay time.Duration, prompt string)
	// OnStop, when set, replaces clearing that wakeup. Stop ends only the
	// self-paced loop; the user's interval jobs keep running.
	OnStop func()
}

// LoopTools builds the scheduling tools over opts.
func LoopTools(opts LoopToolOptions) []agent.AgentTool {
	return []agent.AgentTool{
		scheduleWakeupTool(opts),
		cronCreateTool(opts),
		cronDeleteTool(opts),
		loopListTool(opts),
	}
}

// jsonResult builds a plain-text tool result.
func loopTextResult(text string, isError bool) agent.AgentToolResult {
	return agent.AgentToolResult{
		Content: []ai.Content{ai.TextContent{Text: text}},
		IsError: isError,
	}
}

// scheduleWakeupTool lets a self-paced loop choose when to resume.
func scheduleWakeupTool(opts LoopToolOptions) agent.AgentTool {
	return agent.AgentTool{
		Name:  "schedule_wakeup",
		Label: "Schedule Wakeup",
		Description: "Schedule when to resume work in a self-paced loop, one the user started without an interval. " +
			"Pass the same loop prompt back via prompt each turn so the next firing repeats the task. " +
			"To end the loop, call this with stop set to true and no other field.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"delay_seconds":{"type":"number","description":"Seconds to wait before the next fire. Clamped to [60, 3600]. Ignored when stop is set."},` +
			`"reason":{"type":"string","description":"One short sentence on why this delay was chosen."},` +
			`"prompt":{"type":"string","description":"The full loop input to run again, prefixed with /loop so the next firing re-enters the loop."},` +
			`"stop":{"type":"boolean","description":"End the loop now. Omit every other field."}` +
			`},"additionalProperties":false}`),
		Execute: func(_ context.Context, _ string, params json.RawMessage, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				DelaySeconds float64 `json:"delay_seconds"`
				Reason       string  `json:"reason"`
				Prompt       string  `json:"prompt"`
				Stop         bool    `json:"stop"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return loopTextResult(err.Error(), true), nil
			}
			if input.Stop {
				if opts.OnStop != nil {
					opts.OnStop()
				} else if opts.Scheduler != nil {
					opts.Scheduler.ClearWakeup()
				}
				return loopTextResult("The loop has ended. No further wakeups will fire.", false), nil
			}
			prompt := ai.JSTrim(input.Prompt)
			if prompt == "" {
				return loopTextResult("Pass the loop input via prompt, or stop set to true to end the loop.", true), nil
			}
			delay := time.Duration(input.DelaySeconds * float64(time.Second))
			if delay < loopWakeupMinDelay {
				delay = loopWakeupMinDelay
			}
			if delay > loopWakeupMaxDelay {
				delay = loopWakeupMaxDelay
			}
			if opts.OnWakeup != nil {
				opts.OnWakeup(delay, prompt)
			} else if opts.Scheduler != nil {
				// The next run is told it is a loop too. A slash command such as
				// "/loop check" is left whole: its own command handles it.
				armed := prompt
				if !strings.HasPrefix(strings.TrimSpace(prompt), "/") {
					armed = LoopSelfPacedPrompt(prompt)
				}
				if _, err := opts.Scheduler.SetWakeup(delay, armed); err != nil {
					return loopTextResult(err.Error(), true), nil
				}
			}
			return loopTextResult(fmt.Sprintf("The loop resumes in %s.", humanDuration(delay)), false), nil
		},
	}
}

// cronCreateTool arms a cron-scheduled prompt.
func cronCreateTool(opts LoopToolOptions) agent.AgentTool {
	return agent.AgentTool{
		Name:  "cron_create",
		Label: "Cron Create",
		Description: "Schedule a prompt to run at a future time, either recurring on a cron schedule or once at a specific time. " +
			"Use it when the user asks for something at a set time or at fixed intervals, rather than promising it in a reply.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"cron":{"type":"string","description":"Standard 5-field cron expression in local time: \"M H DoM Mon DoW\" (for example \"*/5 * * * *\" is every 5 minutes)."},` +
			`"prompt":{"type":"string","description":"The prompt to enqueue at each fire time."},` +
			`"recurring":{"type":"boolean","description":"true (default) fires on every match until deleted or expired. false fires once at the next match, then deletes itself."}` +
			`},"required":["cron","prompt"],"additionalProperties":false}`),
		Execute: func(_ context.Context, _ string, params json.RawMessage, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Cron      string `json:"cron"`
				Prompt    string `json:"prompt"`
				Recurring *bool  `json:"recurring"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return loopTextResult(err.Error(), true), nil
			}
			if opts.Scheduler == nil {
				return loopTextResult("Scheduling is not available in this session.", true), nil
			}
			recurring := true
			if input.Recurring != nil {
				recurring = *input.Recurring
			}
			prompt := ai.JSTrim(input.Prompt)
			if prompt == "" {
				return loopTextResult("Pass the prompt to enqueue at each fire time.", true), nil
			}
			job, err := opts.Scheduler.Add(strings.TrimSpace(input.Cron), prompt, recurring, opts.MaxAge)
			if err != nil {
				return loopTextResult(err.Error(), true), nil
			}
			if !recurring {
				return loopTextResult(fmt.Sprintf("Scheduled one-shot task %s (%s). It will fire once then delete itself.",
					job.ID, job.HumanSchedule), false), nil
			}
			return loopTextResult(fmt.Sprintf("Scheduled recurring job %s (%s). Cancel it sooner with cron_delete.",
				job.ID, job.HumanSchedule), false), nil
		},
	}
}

// cronDeleteTool cancels a job.
func cronDeleteTool(opts LoopToolOptions) agent.AgentTool {
	return agent.AgentTool{
		Name:        "cron_delete",
		Label:       "Cron Delete",
		Description: "Cancel a scheduled prompt by its id.",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The id cron_create reported."}` +
			`},"required":["id"],"additionalProperties":false}`),
		Execute: func(_ context.Context, _ string, params json.RawMessage, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(params, &input); err != nil {
				return loopTextResult(err.Error(), true), nil
			}
			if opts.Scheduler == nil || !opts.Scheduler.Remove(strings.TrimSpace(input.ID)) {
				return loopTextResult(fmt.Sprintf("No scheduled job with id %q.", input.ID), true), nil
			}
			return loopTextResult("Cancelled "+input.ID+".", false), nil
		},
	}
}

// loopListTool reports the session's scheduled prompts.
func loopListTool(opts LoopToolOptions) agent.AgentTool {
	return agent.AgentTool{
		Name:        "cron_list",
		Label:       "Cron List",
		Description: "List the prompts scheduled in this session, with their schedule and next fire time.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Execute: func(_ context.Context, _ string, _ json.RawMessage, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			if opts.Scheduler == nil {
				return loopTextResult("No scheduled jobs.", false), nil
			}
			jobs := opts.Scheduler.Jobs()
			if len(jobs) == 0 {
				return loopTextResult("No scheduled jobs.", false), nil
			}
			var lines []string
			for _, job := range jobs {
				kind := "recurring"
				if !job.Recurring {
					kind = "one-shot"
				}
				lines = append(lines, fmt.Sprintf("%s — %s (%s), next run %s: %s",
					job.ID, job.HumanSchedule, kind, job.NextFire.Format(time.RFC3339), job.Prompt))
			}
			return loopTextResult(strings.Join(lines, "\n"), false), nil
		},
	}
}

// humanDuration renders a delay the way a person reads it.
func humanDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		hours := int(d / time.Hour)
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	case d >= time.Minute && d%time.Minute == 0:
		minutes := int(d / time.Minute)
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	default:
		return fmt.Sprintf("%d seconds", int(d/time.Second))
	}
}
