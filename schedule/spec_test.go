package schedule_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestCalendarDSTAndExplicitTimeZones(t *testing.T) {
	zone, err := temporal.ParseTimeZone("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ expression, after, want string }{
		{"30 2 * * *", "2026-03-08T06:59:00Z", "2026-03-09T06:30:00Z"},
		{"30 1 * * *", "2026-11-01T05:00:00Z", "2026-11-01T05:30:00Z"},
		{"30 1 * * *", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"},
		{"0 0 29 FEB *", "2026-01-01T00:00:00Z", "2028-02-29T05:00:00Z"},
	} {
		t.Run(tc.after+tc.expression, func(t *testing.T) {
			spec, err := schedule.ParseCron(tc.expression, zone)
			if err != nil {
				t.Fatal(err)
			}
			after, err := time.Parse(time.RFC3339, tc.after)
			if err != nil {
				t.Fatal(err)
			}
			next, err := spec.Next(after)
			if err != nil || next.Format(time.RFC3339) != tc.want {
				t.Fatal(next, err)
			}
		})
	}
	for _, expression := range []string{"", "0 *", "*/0 * * * *", "0 0 31 FEB *", "0 0 * * 7", "0,,1 * * * *", "@daily", "TZ=UTC", "CRON_TZ=UTC 0 0 * * *", "0 0 0 * * * 2026"} {
		if _, err := schedule.ParseCron(expression, time.UTC); !errors.Is(err, fault.Invalid) {
			t.Fatalf("accepted %q: %v", expression, err)
		}
	}
	for _, zone := range []*time.Location{nil, time.Local} {
		if _, err := schedule.ParseCron("0 * * * *", zone); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestIntervalsAreAnchoredAndPreserveMillisecondPrecision(t *testing.T) {
	anchor := time.Date(2026, 9, 16, 1, 2, 3, 4*int(time.Millisecond), time.UTC)
	spec, err := schedule.IntervalFrom(1500*time.Millisecond, anchor)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ after, want time.Time }{
		{anchor.Add(-time.Second), anchor},
		{anchor, anchor.Add(1500 * time.Millisecond)},
		{anchor.Add(1500*time.Millisecond - time.Nanosecond), anchor.Add(1500 * time.Millisecond)},
		{anchor.Add(3 * time.Second), anchor.Add(4500 * time.Millisecond)},
	} {
		next, err := spec.Next(tc.after)
		if err != nil || !next.Equal(tc.want) {
			t.Fatal(next, err)
		}
	}
	if _, err := schedule.Interval(time.Nanosecond); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := schedule.IntervalFrom(time.Second, anchor.Add(time.Nanosecond)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := (schedule.Spec{}).Next(anchor); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestDeclarationConvenienceAndFrozenOptions(t *testing.T) {
	handler := func(context.Context, schedule.Invocation) error { return nil }
	d, err := schedule.DailyAt("daily.report", "03:05", time.UTC, handler)
	if err != nil {
		t.Fatal(err)
	}
	if d.Spec().String() != "0 5 3 * * *" {
		t.Fatal(d.Spec())
	}
	options := schedule.DefaultOptions()
	options.Environments = []string{"production"}
	d, err = d.With(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Environments[0] = "changed"
	copy := d.Options()
	copy.Environments[0] = "also-changed"
	if d.Options().Environments[0] != "production" {
		t.Fatal("declaration retained caller options")
	}
	if _, err := schedule.NewRegistry(d, d); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := schedule.DailyAt("daily.report", "3:05", time.UTC, handler); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	options.CatchUp = schedule.CatchUp{Window: time.Hour}
	if _, err := d.With(options); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
