package schedule_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/schedule"
	schedulecommand "github.com/weiloon1234/Foundry-Go/schedule/command"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func wallTime(t *testing.T, text string) temporal.Time {
	t.Helper()
	at, err := temporal.ParseTime(text)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// Calendar filters narrow the spec silently: filtered occurrences leave no
// history and use no execution slot.
func TestCalendarFiltersSkipOccurrencesSilently(t *testing.T) {
	var calls atomic.Int32
	options := schedule.DefaultOptions()
	// The fixture clock starts on Wednesday 2026-09-16 at 00:00 UTC.
	options.Days = []time.Weekday{time.Thursday}
	options.Between = schedule.UnlessBetween(wallTime(t, "00:00:00"), wallTime(t, "00:05:00"))
	f := newFixture(t, every(t, "filtered", func(context.Context, schedule.Invocation) error { calls.Add(1); return nil }, options))
	// step advances the clock and returns after the scheduler finished a tick at
	// that time: the third wake is buffered only once the second was consumed,
	// which happens after the tick for the first.
	step := func(d time.Duration) { f.advance(d); f.wake <- struct{}{}; f.wake <- struct{}{} }
	f.start(t)
	waitFor(t, func() bool { return f.scheduler.Snapshot().Leader })
	step(time.Minute)     // Wednesday 00:01: wrong day.
	step(24 * time.Hour)  // Thursday 00:01: the pending Wednesday 00:02 is filtered.
	step(time.Minute)     // Thursday 00:02: inside the excluded window.
	step(4 * time.Minute) // Thursday 00:06: the pending 00:03 is still excluded.
	if calls.Load() != 0 || len(f.scheduler.Snapshot().History) != 0 {
		t.Fatal("filtered occurrence ran or was recorded", calls.Load())
	}
	f.advance(time.Minute) // Thursday 00:07 runs.
	waitFor(t, func() bool { return calls.Load() == 1 })
}

func TestWhenPredicateRecordsFilteredSkip(t *testing.T) {
	var calls atomic.Int32
	allow := atomic.Bool{}
	options := schedule.DefaultOptions()
	options.When = func(context.Context, schedule.Invocation) (bool, error) { return allow.Load(), nil }
	f := newFixture(t, every(t, "predicate", func(context.Context, schedule.Invocation) error { calls.Add(1); return nil }, options))
	f.start(t)
	f.advance(time.Minute)
	waitFor(t, func() bool { h := f.scheduler.Snapshot().History; return len(h) == 1 && h[0].State == schedule.Skipped })
	if record := f.scheduler.Snapshot().History[0]; record.Reason != schedule.Filtered || calls.Load() != 0 {
		t.Fatal("declined predicate did not skip", record.Reason)
	}
	allow.Store(true)
	f.advance(time.Minute)
	waitFor(t, func() bool { return calls.Load() == 1 })
}

func TestLastDayOfMonthAndEveryMinuteHelpers(t *testing.T) {
	handler := func(context.Context, schedule.Invocation) error { return nil }
	last, err := schedule.LastDayOfMonthAt("month.close", "23:30", time.UTC, handler)
	if err != nil || !last.Options().LastDayOfMonth {
		t.Fatal(err)
	}
	five, err := schedule.EveryFiveMinutes("five", time.UTC, handler)
	if err != nil {
		t.Fatal(err)
	}
	next, err := five.Spec().Next(time.Date(2026, 9, 16, 0, 2, 0, 0, time.UTC))
	if err != nil || !next.Equal(time.Date(2026, 9, 16, 0, 5, 0, 0, time.UTC)) {
		t.Fatal("five-minute boundary", next, err)
	}
	for _, invalid := range []schedule.Options{
		{Timeout: time.Minute, OverlapTTL: time.Second, Days: []time.Weekday{time.Monday, time.Monday}},
		{Timeout: time.Minute, OverlapTTL: time.Second, Between: schedule.Between(wallTime(t, "09:00:00"), wallTime(t, "09:00:00"))},
	} {
		if _, err := last.With(invalid); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid filter accepted", err)
		}
	}
}

func TestScheduleTestCommandRunsOneScheduleNow(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t,
		every(t, "reports.daily", func(context.Context, schedule.Invocation) error { calls.Add(1); return nil }),
		every(t, "reports.broken", func(context.Context, schedule.Invocation) error { return errors.New("private failure") }))
	run := func(args ...string) (string, error) {
		t.Helper()
		command, err := schedulecommand.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		err = command.Run(t.Context(), f.scheduler, &output)
		return output.String(), err
	}
	if out, err := run("schedule", "test", "--id", "reports.daily", "--format", "json"); err != nil || !strings.Contains(out, `"state":"succeeded"`) || calls.Load() != 1 {
		t.Fatal("schedule test did not run the handler", out, err)
	}
	out, err := run("schedule", "test", "--id", "reports.broken")
	if err == nil || !strings.Contains(out, "failed") || strings.Contains(out, "private") {
		t.Fatal("failed test run was not reported safely", out, err)
	}
	if _, err := f.scheduler.RunNow(t.Context(), "reports.absent"); !errors.Is(err, fault.Missing) {
		t.Fatal("unknown schedule ran", err)
	}
	if _, err := schedulecommand.Parse([]string{"schedule", "test"}, io.Discard); err == nil {
		t.Fatal("missing --id accepted")
	}
}
