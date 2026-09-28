package temporal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestServiceCalendarDefaultsAndIsolation(t *testing.T) {
	source := fixedHelperClock{time.Date(2026, 9, 25, 17, 30, 0, 0, time.UTC)}
	dates, err := temporal.NewService(source, "Asia/Kuala_Lumpur")
	if err != nil {
		t.Fatal(err)
	}
	utc, err := dates.In(temporal.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// Changing a returned standard location cannot replace the retained zone.
	location := dates.Location()
	*location = *time.UTC
	now, err := dates.Now()
	if err != nil || now.String() != "2026-09-25T17:30:00Z" {
		t.Fatal(now, err)
	}
	today, err := dates.Today()
	if err != nil || today.String() != "2026-09-26" {
		t.Fatal(today, err)
	}
	other, err := utc.Today()
	if err != nil || other.String() != "2026-09-25" {
		t.Fatal(other, err)
	}
	formatted, err := dates.Format(now)
	if err != nil || formatted != "2026-09-26T01:30:00+08:00" {
		t.Fatal(formatted, err)
	}
	parsed, err := dates.Parse("2026-09-26T01:30:00")
	if err != nil || parsed != now {
		t.Fatal(parsed, err)
	}
	explicit, err := dates.Parse("2026-09-25T17:30:00Z")
	if err != nil || explicit != now {
		t.Fatal(explicit, err)
	}
	start, err := dates.StartOfDay(now)
	if err != nil || start.String() != "2026-09-25T16:00:00Z" {
		t.Fatal(start, err)
	}
	if dates.TimeZone() != "Asia/Kuala_Lumpur" || utc.TimeZone() != temporal.UTC {
		t.Fatal("zone override mutated service")
	}
}

func TestServiceDaysRespectDSTAndRejectAmbiguousWallTimes(t *testing.T) {
	dates, err := temporal.NewService(fixedHelperClock{}, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		hours time.Duration
	}{
		{"2026-03-07T12:00:00", 23}, {"2026-10-31T12:00:00", 25},
	} {
		before, err := dates.Parse(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		after, err := dates.AddDays(before, 1)
		if err != nil || after.UTC().Sub(before.UTC()) != tc.hours*time.Hour {
			t.Fatal(after, err)
		}
		back, err := dates.AddDays(after, -1)
		if err != nil || back != before {
			t.Fatal(back, err)
		}
	}
	for _, tc := range []struct {
		before, wall string
		want         error
	}{
		{"2026-03-07T02:30:00", "2026-03-08T02:30:00", fault.Invalid},
		{"2026-10-31T01:30:00", "2026-11-01T01:30:00", fault.Conflict},
	} {
		before, err := dates.Parse(tc.before)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dates.AddDays(before, 1); !errors.Is(err, tc.want) {
			t.Fatal("calendar arithmetic guessed DST", err)
		}
		if _, err := dates.Parse(tc.wall); !errors.Is(err, tc.want) {
			t.Fatal("parser guessed DST", err)
		}
	}
	if _, err := dates.Parse("2026-11-01T01:30:00-04:00"); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRejectsInvalidClockZoneAndZeroValue(t *testing.T) {
	var nilClock *fixedHelperClock
	for _, source := range []clock.Clock{nil, nilClock} {
		if _, err := temporal.NewService(source, temporal.UTC); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	for _, name := range []temporal.ZoneName{"", "Local", "Invalid/Zone", "+24:00"} {
		if _, err := temporal.NewService(fixedHelperClock{}, name); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	var zero temporal.Service
	if _, err := zero.Today(); err == nil {
		t.Fatal("zero service accepted")
	}
	if _, err := zero.Parse("2026-09-25T17:30:00Z"); err == nil {
		t.Fatal("zero service parsed")
	}
}
