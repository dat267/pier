package coding

// The recurring-prompt scheduler behind /loop: jobs that fire a prompt on a cron
// schedule for as long as the session stays open.

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LoopJob is one scheduled prompt. A recurring job fires on every cron match
// until it is cancelled or reaches its maximum age; a one-shot job fires once
// and deletes itself.
type LoopJob struct {
	ID       string
	Cron     string
	Prompt   string
	Schedule CronSchedule
	// HumanSchedule is the label the tools report for Cron.
	HumanSchedule string
	Recurring     bool
	CreatedAt     time.Time
	LastFiredAt   time.Time
	// NextFire is when this job is next due.
	NextFire time.Time
	// MaxAge ends a recurring job; zero means never.
	MaxAge time.Duration
}

// LoopRecurringMaxAge is how long a recurring job lives. The reference expires a
// recurring schedule after 30 days, so a forgotten loop does not run forever.
const LoopRecurringMaxAge = 30 * 24 * time.Hour

// loopMaxJobs is the cap on concurrent jobs. A runaway loop is worse than a
// refused one, so a new schedule past the cap is an error the tool reports.
const loopMaxJobs = 50

// errCronInvalid reports a cron expression that does not parse.
func errCronInvalid(cron string) error {
	return fmt.Errorf("invalid cron expression %q: expected 5 fields: M H DoM Mon DoW", cron)
}

// errCronNever reports a schedule that matches no date in its search window.
func errCronNever(cron string) error {
	return fmt.Errorf("cron expression %q does not match any calendar date in the next year", cron)
}

// errLoopCapReached reports the job cap.
func errLoopCapReached(limit int) error {
	return fmt.Errorf("too many scheduled jobs (max %d); cancel one first", limit)
}

// errSchedulerStopped reports a schedule attempt on a closed session.
func errSchedulerStopped() error {
	return errors.New("the session is closed, so no prompt can be scheduled")
}

// LoopScheduler owns the session's scheduled prompts. A fired prompt is handed
// to the enqueue callback, which the host wires to its own submission path so a
// fire arrives as ordinary user input rather than a special event.
type LoopScheduler struct {
	mu      sync.Mutex
	jobs    []*LoopJob
	nextID  int
	now     func() time.Time
	enqueue func(prompt string)
	timer   *time.Timer
	started bool
	stopped bool
	// wakeupID names the self-paced loop's pending wakeup, "" when none.
	wakeupID string

	// scheduleNudge wakes the timer loop when the job set changes.
	nudge chan struct{}
	done  chan struct{}
}

// NewLoopScheduler creates a scheduler. now defaults to time.Now and enqueue is
// required for a fire to do anything.
func NewLoopScheduler(now func() time.Time, enqueue func(prompt string)) *LoopScheduler {
	if now == nil {
		now = time.Now
	}
	return &LoopScheduler{now: now, enqueue: enqueue, nudge: make(chan struct{}, 1), done: make(chan struct{})}
}

// Start begins the timer loop. It is safe to call once; a second call is a
// no-op.
func (s *LoopScheduler) Start() {
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.run()
}

// Running reports whether the timer loop was started and is not yet stopped.
func (s *LoopScheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && !s.stopped
}

// Stop ends the timer loop and drops every job, so a closed session leaves no
// goroutine and no pending fire.
func (s *LoopScheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.jobs = nil
	s.mu.Unlock()
	close(s.done)
}

// Add schedules a prompt. A cron that cannot be parsed is an error, as is a
// job past the cap or one that never matches.
func (s *LoopScheduler) Add(cron string, prompt string, recurring bool, maxAge time.Duration) (LoopJob, error) {
	schedule, ok := ParseCron(cron)
	if !ok {
		return LoopJob{}, errCronInvalid(cron)
	}
	now := s.now()
	next, ok := s.nextFire(schedule, now, recurring)
	if !ok {
		return LoopJob{}, errCronNever(cron)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return LoopJob{}, errSchedulerStopped()
	}
	if len(s.jobs) >= loopMaxJobs {
		return LoopJob{}, errLoopCapReached(loopMaxJobs)
	}
	s.nextID++
	job := &LoopJob{
		ID:            loopJobID(s.nextID),
		Cron:          cron,
		Prompt:        prompt,
		Schedule:      schedule,
		HumanSchedule: HumanCronSchedule(cron),
		Recurring:     recurring,
		CreatedAt:     now,
		NextFire:      next,
		MaxAge:        maxAge,
	}
	s.jobs = append(s.jobs, job)
	s.wake()
	return *job, nil
}

// SetWakeup arms the self-paced loop's next run: a one-shot job that fires
// prompt after delay. At most one wakeup is pending, so a new one replaces it.
func (s *LoopScheduler) SetWakeup(delay time.Duration, prompt string) (LoopJob, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return LoopJob{}, errSchedulerStopped()
	}
	s.removeLocked(s.wakeupID)
	if len(s.jobs) >= loopMaxJobs {
		return LoopJob{}, errLoopCapReached(loopMaxJobs)
	}
	s.nextID++
	job := &LoopJob{
		ID:            loopJobID(s.nextID),
		Prompt:        prompt,
		HumanSchedule: "Once, in " + humanDuration(delay),
		CreatedAt:     now,
		NextFire:      now.Add(delay),
	}
	s.jobs = append(s.jobs, job)
	s.wakeupID = job.ID
	s.wake()
	return *job, nil
}

// ClearWakeup cancels the pending self-paced wakeup, leaving every other job,
// and reports whether there was one.
func (s *LoopScheduler) ClearWakeup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := s.removeLocked(s.wakeupID)
	s.wakeupID = ""
	if removed {
		s.wake()
	}
	return removed
}

// removeLocked drops the job named id, reporting whether it existed.
func (s *LoopScheduler) removeLocked(id string) bool {
	if id == "" {
		return false
	}
	for index, job := range s.jobs {
		if job.ID == id {
			s.jobs = slices.Delete(s.jobs, index, index+1)
			return true
		}
	}
	return false
}

// Remove cancels a job by id and reports whether it existed.
func (s *LoopScheduler) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.removeLocked(id) {
		return false
	}
	s.wake()
	return true
}

// Jobs lists the scheduled jobs in creation order.
func (s *LoopScheduler) Jobs() []LoopJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LoopJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, *job)
	}
	return out
}

// RemoveAll cancels every job, which is what a loop's stop request does.
func (s *LoopScheduler) RemoveAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = nil
	s.wake()
}

// wake nudges the timer loop without blocking.
func (s *LoopScheduler) wake() {
	select {
	case s.nudge <- struct{}{}:
	default:
	}
}

// run fires jobs as their time arrives. It waits on a timer for the earliest
// job, so an idle session with no jobs holds no timer at all.
func (s *LoopScheduler) run() {
	for {
		wait, ok := s.nextWait()
		if !ok {
			select {
			case <-s.done:
				return
			case <-s.nudge:
				continue
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.done:
			timer.Stop()
			return
		case <-s.nudge:
			timer.Stop()
			continue
		case <-timer.C:
			s.fireDue()
		}
	}
}

// nextWait reports how long until the earliest job is due.
func (s *LoopScheduler) nextWait() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) == 0 {
		return 0, false
	}
	now := s.now()
	var earliest time.Time
	for _, job := range s.jobs {
		if earliest.IsZero() || job.NextFire.Before(earliest) {
			earliest = job.NextFire
		}
	}
	if earliest.IsZero() {
		return 0, false
	}
	wait := earliest.Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

// fireDue enqueues every job that is due and advances or deletes each of them.
func (s *LoopScheduler) fireDue() {
	s.mu.Lock()
	now := s.now()
	var prompts []string
	kept := s.jobs[:0]
	for _, job := range s.jobs {
		if job.NextFire.After(now) {
			kept = append(kept, job)
			continue
		}
		prompts = append(prompts, job.Prompt)
		job.LastFiredAt = now
		// A one-shot job is done after one fire. A recurring job advances to its
		// next match and ends once that match falls past its maximum age.
		if !job.Recurring {
			continue
		}
		next, ok := job.Schedule.NextCronTime(job.NextFire)
		if !ok {
			continue
		}
		if job.MaxAge > 0 && next.Sub(job.CreatedAt) >= job.MaxAge {
			continue
		}
		job.NextFire = next
		kept = append(kept, job)
	}
	s.jobs = kept
	enqueue := s.enqueue
	s.mu.Unlock()
	if enqueue == nil {
		return
	}
	for _, prompt := range prompts {
		enqueue(prompt)
	}
}

// nextFire reports the next match after now for a schedule.
func (s *LoopScheduler) nextFire(schedule CronSchedule, now time.Time, recurring bool) (time.Time, bool) {
	if recurring {
		return schedule.NextCronTime(now)
	}
	return schedule.NextCronOneShotTime(now)
}

// loopJobID builds a short job id.
func loopJobID(sequence int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	if sequence <= 0 {
		return "0"
	}
	var digits []byte
	for sequence > 0 {
		digits = append(digits, alphabet[sequence%len(alphabet)])
		sequence /= len(alphabet)
	}
	slices.Reverse(digits)
	return string(digits)
}

// ParseLoopInput splits `/loop [interval] <prompt>` the way the command does: a
// leading `5m`/`2h` token, else a trailing `every 20m`/`every 5 minutes` clause,
// else no interval at all, in which case the whole input is the prompt and the
// loop self-paces. It reports the interval (zero when none) and the prompt.
//
// Examples: `5m /babysit-prs` gives 5m and `/babysit-prs`; `check the deploy
// every 20m` gives 20m and `check the deploy`; `check every PR` has no interval
// because what follows "every" is not a time expression.
func ParseLoopInput(input string) (time.Duration, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		if interval, ok := parseLoopInterval(fields[0]); ok && len(fields) > 1 {
			return interval, strings.TrimSpace(trimmed[len(fields[0]):])
		}
	}
	if interval, prompt, ok := stripTrailingEvery(trimmed); ok {
		return interval, prompt
	}
	return 0, trimmed
}

// parseLoopInterval reads a `5m`/`2h`/`30s`/`1d` token.
func parseLoopInterval(token string) (time.Duration, bool) {
	if len(token) < 2 {
		return 0, false
	}
	number := token[:len(token)-1]
	unit := token[len(token)-1]
	value, ok := parsePositiveInt(number)
	if !ok {
		return 0, false
	}
	switch unit {
	case 's':
		return time.Duration(value) * time.Second, true
	case 'm':
		return time.Duration(value) * time.Minute, true
	case 'h':
		return time.Duration(value) * time.Hour, true
	case 'd':
		return time.Duration(value) * 24 * time.Hour, true
	}
	return 0, false
}

// stripTrailingEvery reads a trailing `every <N><unit>` or `every <N> <word>`.
func stripTrailingEvery(input string) (time.Duration, string, bool) {
	fields := strings.Fields(input)
	if len(fields) < 2 {
		return 0, "", false
	}
	// The clause is the last one or two tokens, found by scanning backwards for
	// the literal `every`.
	for index := len(fields) - 1; index >= 0; index-- {
		if fields[index] != "every" || index == len(fields)-1 {
			continue
		}
		rest := fields[index+1:]
		if len(rest) > 2 {
			continue
		}
		interval, ok := parseEveryClause(rest)
		if !ok {
			continue
		}
		prompt := strings.TrimSpace(strings.Join(fields[:index], " "))
		if prompt == "" {
			return 0, "", false
		}
		return interval, prompt, true
	}
	return 0, "", false
}

// parseEveryClause reads `<N><unit>` or `<N> <unit>`.
func parseEveryClause(fields []string) (time.Duration, bool) {
	if len(fields) == 1 {
		return parseLoopInterval(fields[0])
	}
	value, ok := parsePositiveInt(fields[0])
	if !ok {
		return 0, false
	}
	// The unit word may be plural: "5 minutes", "2 hours".
	unit := fields[1]
	if trimmed, found := strings.CutSuffix(unit, "s"); found && trimmed != "" {
		unit = trimmed
	}
	switch unit {
	case "second":
		return time.Duration(value) * time.Second, true
	case "minute":
		return time.Duration(value) * time.Minute, true
	case "hour":
		return time.Duration(value) * time.Hour, true
	case "day":
		return time.Duration(value) * 24 * time.Hour, true
	}
	return 0, false
}

// parsePositiveInt reads a decimal integer above zero.
func parsePositiveInt(text string) (int, bool) {
	value, err := strconv.Atoi(text)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// LoopCronForInterval converts an interval into a cron expression and reports
// the interval it actually runs at. A cron step only gives even gaps when it
// divides its unit (*/45 fires at :00 and :45), so the interval snaps to the
// nearest step that does: minutes to a divisor of 60, hours to a divisor of 24,
// and days to whole days, with ties going to the longer step. Seconds round up
// to a whole minute. Zero, or more than 28 days, reports false.
func LoopCronForInterval(interval time.Duration) (string, time.Duration, bool) {
	if interval <= 0 {
		return "", 0, false
	}
	minutes := (interval + time.Minute - 1) / time.Minute
	if minutes < 60 {
		if step := nearestLoopStep(float64(minutes), loopMinuteSteps); step < 60 {
			return "*/" + strconv.Itoa(step) + " * * * *", time.Duration(step) * time.Minute, true
		}
		return "0 */1 * * *", time.Hour, true
	}
	hours := float64(minutes) / 60
	if hours < 24 {
		if step := nearestLoopStep(hours, loopHourSteps); step < 24 {
			return "0 */" + strconv.Itoa(step) + " * * *", time.Duration(step) * time.Hour, true
		}
		return "0 0 * * *", 24 * time.Hour, true
	}
	days := int(math.Round(hours / 24))
	if days > 28 {
		return "", 0, false
	}
	used := time.Duration(days) * 24 * time.Hour
	if days == 1 {
		return "0 0 * * *", used, true
	}
	return "0 0 */" + strconv.Itoa(days) + " * *", used, true
}

// The steps that divide an hour and a day evenly. Each ends with its whole
// unit, which LoopCronForInterval promotes to the next field.
var (
	loopMinuteSteps = []int{1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60}
	loopHourSteps   = []int{1, 2, 3, 4, 6, 8, 12, 24}
)

// nearestLoopStep is the step closest to value, the longer one on a tie.
func nearestLoopStep(value float64, steps []int) int {
	best := steps[0]
	for _, step := range steps[1:] {
		if math.Abs(float64(step)-value) <= math.Abs(float64(best)-value) {
			best = step
		}
	}
	return best
}
