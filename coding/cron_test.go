package coding

import (
	"testing"
	"time"
)

// A cron field is `*`, `*/n`, `a`, `a-b`, `a-b/n`, or a comma list of those;
// bounds are per field, and day-of-week accepts 7 as Sunday, normalised to 0.
func TestParseCronFields(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ok   bool
	}{
		{"every minute", "* * * * *", true},
		{"step minutes", "*/5 * * * *", true},
		{"step of one", "*/1 * * * *", true},
		{"plain minute", "30 * * * *", true},
		{"hour step", "0 */6 * * *", true},
		{"range", "0 9-17 * * *", true},
		{"range with step", "0 0-23/2 * * *", true},
		{"comma list", "0,30 * * * *", true},
		{"mixed list", "0,15-20 * * * *", true},
		{"weekdays", "0 9 * * 1-5", true},
		{"sunday as 7", "0 9 * * 7", true},
		{"month and day", "30 14 28 2 *", true},
		{"four fields", "* * * *", false},
		{"six fields", "* * * * * *", false},
		{"minute out of range", "60 * * * *", false},
		{"hour out of range", "0 24 * * *", false},
		{"day of month zero", "0 0 0 * *", false},
		{"month thirteen", "0 0 * 13 *", false},
		{"weekday eight", "0 0 * * 8", false},
		{"reversed range", "0 17-9 * * *", false},
		{"zero step", "*/0 * * * *", false},
		{"negative range bound", "0 -1 * * *", false},
		{"word", "0 0 * * MON", false},
		{"empty", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := ParseCron(test.expr); ok != test.ok {
				t.Fatalf("ParseCron(%q) ok = %v, want %v", test.expr, ok, test.ok)
			}
		})
	}
}

// Day-of-week 7 is normalised to 0, so a job naming Sunday either way fires on
// the same day.
func TestParseCronSundayIsZero(t *testing.T) {
	seven, ok := ParseCron("0 9 * * 7")
	if !ok {
		t.Fatal("7 is not accepted as Sunday")
	}
	zero, ok := ParseCron("0 9 * * 0")
	if !ok {
		t.Fatal("0 is not accepted as Sunday")
	}
	if len(seven.dayOfWeek) != 1 || seven.dayOfWeek[0] != 0 {
		t.Fatalf("dayOfWeek = %v, want [0]", seven.dayOfWeek)
	}
	if len(zero.dayOfWeek) != 1 || zero.dayOfWeek[0] != 0 {
		t.Fatalf("dayOfWeek = %v, want [0]", zero.dayOfWeek)
	}
}

// A search starts at the next whole minute, so a time exactly on a matching
// minute reports the following one.
func TestNextCronTimeStartsAtTheNextMinute(t *testing.T) {
	schedule, ok := ParseCron("*/5 * * * *")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	base := time.Date(2026, time.November, 25, 10, 30, 0, 0, time.UTC)
	got, ok := schedule.NextCronTime(base)
	if !ok {
		t.Fatal("no next time")
	}
	want := time.Date(2026, time.November, 25, 10, 35, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

// A sparse schedule skips whole months, days and hours that no field admits, so
// it still resolves quickly.
func TestNextCronTimeSkipsWholeUnits(t *testing.T) {
	schedule, ok := ParseCron("30 2 29 2 *")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	// 2027 is not a leap year, so the next 29 February is 2028.
	base := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	got, ok := schedule.NextCronTime(base)
	if !ok {
		t.Fatal("no next time")
	}
	want := time.Date(2028, time.February, 29, 2, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

// Day-of-month and day-of-week union when both are constrained, and only the
// constrained one is checked otherwise. The base
// is chosen so the union and the intersection differ: the next Friday is
// 2026-11-27, while the next 13th that is also a Friday is 2027-08-13.
func TestNextCronTimeDayUnionRule(t *testing.T) {
	schedule, ok := ParseCron("0 0 13 * 5")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	base := time.Date(2026, time.November, 26, 0, 0, 0, 0, time.UTC)
	next, ok := schedule.NextCronTime(base)
	if !ok {
		t.Fatal("no next time")
	}
	want := time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

// Only the constrained day field is checked, so `13 * *` means the 13th whatever
// weekday it is, and `* * 5` means any Friday.
func TestNextCronTimeSingleDayField(t *testing.T) {
	base := time.Date(2026, time.November, 26, 0, 0, 0, 0, time.UTC)
	byMonthDay, ok := ParseCron("0 0 13 * *")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	next, ok := byMonthDay.NextCronTime(base)
	if !ok {
		t.Fatal("no next time")
	}
	if want := time.Date(2026, time.December, 13, 0, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
	byWeekday, ok := ParseCron("0 0 * * 5")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	next, ok = byWeekday.NextCronTime(base)
	if !ok {
		t.Fatal("no next time")
	}
	if want := time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

// The search gives up after 527,040 minutes (a leap year).
func TestNextCronTimeReportsNoMatch(t *testing.T) {
	// 31 February never occurs.
	schedule, ok := ParseCron("0 0 31 2 *")
	if !ok {
		t.Fatal("schedule did not parse")
	}
	if _, ok := schedule.NextCronTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("a 31 February schedule found a match")
	}
}

// The plain phrases the schedule label emits, with the expression itself as the
// fallback for a shape it has no phrase for.
func TestHumanCronSchedule(t *testing.T) {
	tests := []struct{ expr, want string }{
		{"* * * * *", "Every minute"},
		{"*/1 * * * *", "Every minute"},
		{"*/5 * * * *", "Every 5 minutes"},
		{"0 * * * *", "Every hour"},
		{"30 * * * *", "Every hour at :30"},
		{"0 */6 * * *", "Every 6 hours"},
		{"30 */2 * * *", "Every 2 hours at :30"},
		{"0 */1 * * *", "Every hour"},
		{"30 14 * * *", "Every day at 2:30 PM"},
		{"0 0 * * *", "Every day at 12:00 AM"},
		{"0 12 * * *", "Every day at 12:00 PM"},
		{"0 9 * * 1", "Every Monday at 9:00 AM"},
		{"0 9 * * 0", "Every Sunday at 9:00 AM"},
		{"0 9 * * 7", "Every Sunday at 9:00 AM"},
		{"0 9 * * 1-5", "Weekdays at 9:00 AM"},
		// D537: /loop 2d schedules a day step; it read as the raw expression.
		{"0 0 */2 * *", "Every 2 days at 12:00 AM"},
		{"30 9 */3 * *", "Every 3 days at 9:30 AM"},
		{"0 0 */1 * *", "Every day at 12:00 AM"},
		// The 14th at 2:30pm: a one-shot shape with no plain phrase.
		{"30 14 * * 3", "Every Wednesday at 2:30 PM"},
		{"30 14 14 * *", "30 14 14 * *"},
		{"0 9 1 1 *", "0 9 1 1 *"},
		{"not a cron", "not a cron"},
	}
	for _, test := range tests {
		t.Run(test.expr, func(t *testing.T) {
			if got := HumanCronSchedule(test.expr); got != test.want {
				t.Fatalf("HumanCronSchedule(%q) = %q, want %q", test.expr, got, test.want)
			}
		})
	}
}

// A daily schedule in the hour a DST spring-forward skips must still find its
// next fire. The search jumped a non-matching hour by rebuilding the time with
// hour+1, and in the skipped hour that lands back on the same wall clock, so the
// cursor never advanced: the walk burned its whole budget and reported no match.
// Every 02:xx daily job was dead in any DST zone, and a schedule the tool had
// accepted then never fired.
func TestCronFindsNextFireAcrossDSTGap(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no time zone data: %v", err)
	}
	schedule, ok := ParseCron("30 2 * * *")
	if !ok {
		t.Fatal("ParseCron rejected 30 2 * * *")
	}
	// 2026-03-08 is the US spring-forward day: 02:00-03:00 does not exist.
	next, ok := schedule.NextCronTime(time.Date(2026, time.March, 8, 0, 0, 0, 0, zone))
	if !ok {
		t.Fatal("a daily 02:30 schedule found no next fire on a spring-forward day")
	}
	if got := next.Format("2006-01-02 15:04 MST"); got != "2026-03-08 03:30 EDT" && got != "2026-03-09 02:30 EDT" {
		t.Fatalf("next = %s, want the job to move past the gap", got)
	}
}
