package coding

// Port of utils/cron.ts: 5-field cron parsing, next-match search and the human
// schedule label the scheduled-task tools report.
//
// The reference's field grammar is the crate-free one in utils/cron.ts: `*`,
// `*/n`, `a`, `a-b`, `a-b/n` and comma lists of those. Day-of-week accepts 7 as
// Sunday and is normalised to 0, so a job may name Sunday either way.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cronFieldBounds are the inclusive bounds of the five cron fields.
var cronFieldBounds = [5]struct{ min, max int }{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 6},  // day of week
}

// CronSchedule is a parsed 5-field cron expression. Each field is the sorted set
// of values it matches.
type CronSchedule struct {
	minute     []int
	hour       []int
	dayOfMonth []int
	month      []int
	dayOfWeek  []int
}

// ParseCron parses a 5-field cron expression. It reports false when the
// expression is not five whitespace-separated fields or a field is malformed.
func ParseCron(expression string) (CronSchedule, bool) {
	fields := strings.Fields(strings.TrimSpace(expression))
	if len(fields) != 5 {
		return CronSchedule{}, false
	}
	sets := make([][]int, 5)
	for i, field := range fields {
		values, ok := parseCronField(field, cronFieldBounds[i].min, cronFieldBounds[i].max)
		if !ok {
			return CronSchedule{}, false
		}
		sets[i] = values
	}
	return CronSchedule{
		minute: sets[0], hour: sets[1], dayOfMonth: sets[2],
		month: sets[3], dayOfWeek: sets[4],
	}, true
}

// parseCronField expands one comma-separated cron field into its values.
func parseCronField(field string, min, max int) ([]int, bool) {
	isDayOfWeek := min == 0 && max == 6
	seen := map[int]bool{}
	set := func(value int) bool {
		if value < min || value > max {
			return false
		}
		seen[value] = true
		return true
	}
	for _, part := range strings.Split(field, ",") {
		if rest, ok := strings.CutPrefix(part, "*"); ok {
			step := 1
			if rest != "" {
				suffix, hasStep := strings.CutPrefix(rest, "/")
				if !hasStep {
					return nil, false
				}
				parsed, err := strconv.Atoi(suffix)
				if err != nil || parsed < 1 {
					return nil, false
				}
				step = parsed
			}
			for value := min; value <= max; value += step {
				set(value)
			}
			continue
		}
		if low, high, step, ok := parseCronRange(part); ok {
			// A day-of-week range may end at 7, which is Sunday.
			limit := max
			if isDayOfWeek {
				limit = 7
			}
			if low > high || step < 1 || low < min || high > limit {
				return nil, false
			}
			for value := low; value <= high; value += step {
				if isDayOfWeek && value == 7 {
					value = 0
				}
				set(value)
			}
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		if isDayOfWeek && value == 7 {
			value = 0
		}
		if !set(value) {
			return nil, false
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	values := make([]int, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	slices.Sort(values)
	return values, true
}

// parseCronRange splits `a-b` or `a-b/n`.
func parseCronRange(part string) (low, high, step int, ok bool) {
	body := part
	step = 1
	if before, after, hasStep := strings.Cut(part, "/"); hasStep {
		parsed, err := strconv.Atoi(after)
		if err != nil {
			return 0, 0, 0, false
		}
		body, step = before, parsed
	}
	left, right, hasDash := strings.Cut(body, "-")
	if !hasDash {
		return 0, 0, 0, false
	}
	lowValue, err := strconv.Atoi(left)
	if err != nil {
		return 0, 0, 0, false
	}
	highValue, err := strconv.Atoi(right)
	if err != nil {
		return 0, 0, 0, false
	}
	return lowValue, highValue, step, true
}

// NextCronTime returns the first time at or after from that the schedule
// matches, rounded up to the next whole minute. It reports false when no match
// falls within the reference's search window of a leap year in minutes.
func (c CronSchedule) NextCronTime(from time.Time) (time.Time, bool) {
	return c.next(from, false, cronSearchMinutes)
}

// NextCronOneShotTime returns the first match for a one-shot job, which the
// reference searches minute by minute so a second-precision start cannot skip a
// match on the same minute.
func (c CronSchedule) NextCronOneShotTime(from time.Time) (time.Time, bool) {
	return c.next(from, true, cronSearchMinutes)
}

// cronSearchMinutes is the reference's cap, 527,040 minutes (a leap year).
const cronSearchMinutes = 527040

// next walks forward a minute at a time from the minute after from. The
// reference's own loop does the same; keeping the walk (rather than solving the
// fields directly) means the day-of-month/day-of-week union rule below reads
// exactly like utils/cron.ts.
func (c CronSchedule) next(from time.Time, oneShot bool, limit int) (time.Time, bool) {
	if from.IsZero() {
		return time.Time{}, false
	}
	// Floor to the minute, then step forward: a cron time is always a whole
	// minute, and the reference starts at the next one.
	cursor := from.Truncate(time.Minute).Add(time.Minute)
	dayOfMonthFull := len(c.dayOfMonth) == 31
	dayOfWeekFull := len(c.dayOfWeek) == 7
	dayMatches := func(at time.Time) bool {
		// A field left at `*` does not constrain the day. When both are set the
		// reference unions them; when only one is set it is the only one checked.
		if dayOfMonthFull && dayOfWeekFull {
			return true
		}
		if dayOfMonthFull {
			return containsInt(c.dayOfWeek, int(at.Weekday()))
		}
		if dayOfWeekFull {
			return containsInt(c.dayOfMonth, at.Day())
		}
		return containsInt(c.dayOfMonth, at.Day()) || containsInt(c.dayOfWeek, int(at.Weekday()))
	}
	for i := 0; i < limit; i++ {
		// A one-shot job only needs minute resolution, and the reference steps a
		// minute at a time to honour that; the coarse jumps below are a recurring
		// job's shortcut and would skip a same-minute match.
		if !oneShot {
			if !containsInt(c.month, int(cursor.Month())) {
				cursor = time.Date(cursor.Year(), cursor.Month()+1, 1, 0, 0, 0, 0, cursor.Location())
				continue
			}
			if !dayMatches(cursor) {
				cursor = time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, cursor.Location())
				continue
			}
			if !containsInt(c.hour, cursor.Hour()) {
				// Advance by a real hour, not by rebuilding the wall clock: in a
				// DST spring-forward gap the skipped hour does not exist, so
				// time.Date normalises 02:00 back to 01:00 and the cursor would
				// never move, burning the whole search and reporting no match.
				cursor = cursor.Truncate(time.Hour).Add(time.Hour)
				continue
			}
			if !containsInt(c.minute, cursor.Minute()) {
				cursor = cursor.Add(time.Minute)
				continue
			}
			return cursor, true
		}
		if containsInt(c.month, int(cursor.Month())) && dayMatches(cursor) &&
			containsInt(c.hour, cursor.Hour()) && containsInt(c.minute, cursor.Minute()) {
			return cursor, true
		}
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, false
}

// HumanCronSchedule renders a schedule the way the scheduled-task tools report
// it, falling back to the expression itself for shapes with no plain phrase.
func HumanCronSchedule(expression string) string {
	fields := strings.Fields(strings.TrimSpace(expression))
	if len(fields) != 5 {
		return expression
	}
	minute, hour, dayOfMonth, month, dayOfWeek := fields[0], fields[1], fields[2], fields[3], fields[4]
	allDays := dayOfMonth == "*" && month == "*" && dayOfWeek == "*"
	if allDays && hour == "*" {
		if minute == "*" {
			return "Every minute"
		}
		if step, ok := cronStep(minute); ok {
			if step == 1 {
				return "Every minute"
			}
			return fmt.Sprintf("Every %d minutes", step)
		}
	}
	if isAllDigits(minute) && hour == "*" && allDays {
		value, _ := strconv.Atoi(minute)
		if value == 0 {
			return "Every hour"
		}
		return fmt.Sprintf("Every hour at :%02d", value)
	}
	if isAllDigits(minute) && allDays {
		if step, ok := cronStep(hour); ok {
			value, _ := strconv.Atoi(minute)
			suffix := ""
			if value != 0 {
				suffix = fmt.Sprintf(" at :%02d", value)
			}
			if step == 1 {
				return "Every hour" + suffix
			}
			return fmt.Sprintf("Every %d hours%s", step, suffix)
		}
	}
	if !isAllDigits(minute) || !isAllDigits(hour) {
		return expression
	}
	minuteValue, _ := strconv.Atoi(minute)
	hourValue, _ := strconv.Atoi(hour)
	at := humanCronClock(minuteValue, hourValue)
	if dayOfMonth == "*" && month == "*" && dayOfWeek == "*" {
		return "Every day at " + at
	}
	if dayOfMonth == "*" && month == "*" && len(dayOfWeek) == 1 && isAllDigits(dayOfWeek) {
		value, _ := strconv.Atoi(dayOfWeek)
		if name := cronWeekdayName(value % 7); name != "" {
			return fmt.Sprintf("Every %s at %s", name, at)
		}
	}
	if dayOfMonth == "*" && month == "*" && dayOfWeek == "1-5" {
		return "Weekdays at " + at
	}
	// A day-of-month step, which /loop Nd schedules (D537).
	if step, ok := cronStep(dayOfMonth); ok && month == "*" && dayOfWeek == "*" {
		if step == 1 {
			return "Every day at " + at
		}
		return fmt.Sprintf("Every %d days at %s", step, at)
	}
	return expression
}

// cronStep reads a `*/n` field step.
func cronStep(field string) (int, bool) {
	rest, ok := strings.CutPrefix(field, "*/")
	if !ok {
		return 0, false
	}
	step, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return step, true
}

// isAllDigits reports whether the field is one plain number.
func isAllDigits(field string) bool {
	if field == "" {
		return false
	}
	for _, r := range field {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// humanCronClock renders a minute/hour pair as the reference's
// `toLocaleTimeString("en-US", {hour: "numeric", minute: "2-digit"})` does, which
// is a 12-hour clock with a numeric hour: "2:30 PM", "12:05 AM".
func humanCronClock(minute, hour int) string {
	suffix := "AM"
	display := hour
	if hour >= 12 {
		suffix = "PM"
	}
	if display > 12 {
		display -= 12
	}
	if display == 0 {
		display = 12
	}
	return fmt.Sprintf("%d:%02d %s", display, minute, suffix)
}

// cronWeekdayName names a day of week index, matching the reference's table.
func cronWeekdayName(index int) string {
	names := [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	if index < 0 || index >= len(names) {
		return ""
	}
	return names[index]
}

// containsInt reports whether values holds target.
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
