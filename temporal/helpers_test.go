package temporal_test

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type fixedHelperClock struct{ instant time.Time }

func (c fixedHelperClock) Now() time.Time { return c.instant }

type exitingHelperClock struct{}

func (exitingHelperClock) Now() time.Time { runtime.Goexit(); return time.Time{} }

func TestTemporalHelpersUseExplicitClockAndExistingDSTPolicy(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	source := fixedHelperClock{time.Date(2026, 3, 8, 4, 30, 0, 123456000, time.UTC)}
	now, err := temporal.Now(source)
	if err != nil {
		t.Fatal(err)
	}
	today, err := temporal.Today(source, zone)
	if err != nil || today.String() != "2026-03-07" {
		t.Fatal(today, err)
	}
	formatted, err := now.FormatIn(zone)
	if err != nil || formatted != "2026-03-07T23:30:00.123456-05:00" {
		t.Fatal(formatted, err)
	}
	parsed, err := temporal.ParseDateTimeIn(formatted, zone)
	if err != nil || parsed != now {
		t.Fatal(parsed, err)
	}
	if now.UnixMilli() != source.instant.UnixMilli() || now.UnixMicro() != source.instant.UnixMicro() {
		t.Fatal("timestamp conversion changed")
	}
	if _, err := temporal.ParseDateTimeIn("2026-03-08T02:30:00", zone); !errors.Is(err, fault.Invalid) {
		t.Fatal("gap accepted", err)
	}
	if _, err := temporal.ParseDateTimeIn("2026-11-01T01:30:00", zone); !errors.Is(err, fault.Conflict) {
		t.Fatal("overlap picked an arbitrary instant", err)
	}
	if _, err := temporal.ParseDateTimeIn("2026-11-01T01:30:00-04:00", zone); err != nil {
		t.Fatal(err)
	}
	if _, err := temporal.Now(exitingHelperClock{}); err == nil {
		t.Fatal("clock Goexit lost caller")
	}
	if _, err := temporal.Now(nil); err == nil {
		t.Fatal("nil clock accepted")
	}
	var nilClock *fixedHelperClock
	if _, err := temporal.Now(nilClock); err == nil {
		t.Fatal("typed nil clock accepted")
	}
	if _, err := now.FormatIn(time.FixedZone("seconds", 1)); err == nil {
		t.Fatal("offset precision silently lost")
	}
}
