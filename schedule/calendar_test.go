package schedule_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestCalendarDefaultsOverridesAndDST(t *testing.T) {
	handler := func(context.Context, schedule.Invocation) error { return nil }
	for _, tc := range []struct {
		zone              temporal.ZoneName
		wall, after, want string
	}{
		{"Asia/Kuala_Lumpur", "00:00", "2026-09-25T15:59:59Z", "2026-09-25T16:00:00Z"},
		{temporal.UTC, "00:00", "2026-09-25T15:59:59Z", "2026-09-26T00:00:00Z"},
		{"America/New_York", "02:30", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00Z"},
		{"America/New_York", "01:30", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"},
	} {
		calendar, err := schedule.NewCalendar("Asia/Kuala_Lumpur")
		if err != nil {
			t.Fatal(err)
		}
		override, err := calendar.In(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := override.DailyAt("daily", tc.wall, handler)
		if err != nil {
			t.Fatal(err)
		}
		after, err := time.Parse(time.RFC3339, tc.after)
		if err != nil {
			t.Fatal(err)
		}
		next, err := declaration.Spec().Next(after)
		if err != nil || next.Format(time.RFC3339) != tc.want {
			t.Fatal(next, err)
		}
		original, err := calendar.Daily("original", handler)
		if err != nil || original.Spec().TimeZone() != "Asia/Kuala_Lumpur" {
			t.Fatal("override mutated original", err)
		}
	}
	if _, err := schedule.NewCalendar("Local"); err == nil {
		t.Fatal("host timezone accepted")
	}
	var zero schedule.Calendar
	if _, err := zero.Daily("invalid", handler); err == nil {
		t.Fatal("zero calendar accepted")
	}
}
