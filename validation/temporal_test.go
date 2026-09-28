package validation_test

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestTemporalBoundsPreserveInstantsCalendarAndNanoseconds(t *testing.T) {
	t.Parallel()
	at, err := temporal.ParseDateTime("2026-01-02T08:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	same, err := temporal.ParseDateTime("2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.Before(at).Check(t.Context(), same, validation.DefaultLimits()))
	if err := validation.BeforeOrEqual(at).Check(t.Context(), same, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	later, err := at.Add(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.After(at).Check(t.Context(), later, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	info, err := validation.Before(at).Description()
	if err != nil || string(info.Spec.Parameters[1].Value) != `"2026-01-02T00:00:00Z"` {
		t.Fatal("instant metadata differs from runtime", info, err)
	}

	date, err := temporal.ParseDate("2024-02-29")
	if err != nil {
		t.Fatal(err)
	}
	next, err := date.AddDays(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Before(next).Check(t.Context(), date, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.Before(next).Check(t.Context(), temporal.Date{}, validation.DefaultLimits()))
	if validation.Before(temporal.Date{}).Validate() == nil {
		t.Fatal("absent date bound accepted")
	}
	clock, err := temporal.ParseTime("00:00:00.000000001")
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Before(clock).Check(t.Context(), temporal.Time{}, validation.DefaultLimits()); err != nil {
		t.Fatal("midnight treated as absence", err)
	}
	local, err := temporal.NewLocalDateTime(date, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.AfterOrEqual(local).Check(t.Context(), local, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if validation.After(temporal.LocalDateTime{}).Validate() == nil {
		t.Fatal("absent wall date-time bound accepted")
	}
	if err := validation.AfterOrEqual(temporal.DateTime{}).Check(t.Context(), temporal.DateTime{}, validation.DefaultLimits()); err != nil {
		t.Fatal("zero instant treated as absence", err)
	}
}

func TestTemporalFieldComparisonsKeepPathsAndUseExplicitInstants(t *testing.T) {
	t.Parallel()
	type window struct{ Start, End time.Time }
	start := validation.DefineField("start", func(v window) time.Time { return v.Start })
	end := validation.DefineField("end", func(v window) time.Time { return v.End })
	rule := validation.BeforeField(start, end)
	first := time.Date(2026, 1, 2, 0, 0, 0, 0, time.FixedZone("east", 8*3600))
	input := window{first, first.Add(time.Nanosecond).UTC()}
	if err := rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	input.End = first.UTC()
	issues := rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/start" {
		t.Fatal("temporal field path", issues)
	}
	info, err := rule.Description()
	if err != nil || info.Field != "start" || info.OtherField != "end" {
		t.Fatal("temporal field metadata", info, err)
	}
	if err := validation.AfterOrEqualField(end, start).Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if validation.Before(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)).Validate() == nil {
		t.Fatal("out-of-range instant bound accepted")
	}
	input.Start = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits()))
}
