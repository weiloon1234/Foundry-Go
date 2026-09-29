package validation_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// movingClock is an injected application clock tests can advance between checks.
type movingClock struct{ at atomic.Pointer[time.Time] }

func (c *movingClock) Now() time.Time   { return *c.at.Load() }
func (c *movingClock) set(at time.Time) { c.at.Store(&at) }

func TestRelativeTemporalRulesReadTheClockAtCheckTime(t *testing.T) {
	t.Parallel()
	source := &movingClock{}
	// 2026-03-01 23:30 in Kuala Lumpur is still 2026-03-01 15:30 UTC.
	source.set(time.Date(2026, time.March, 1, 15, 30, 0, 0, time.UTC))
	service, err := temporal.NewService(source, "Asia/Kuala_Lumpur")
	if err != nil {
		t.Fatal(err)
	}
	future := validation.AfterNow[temporal.DateTime](service)
	at, err := temporal.NewDateTime(time.Date(2026, time.March, 1, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := future.Check(t.Context(), at, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	// The same declaration observes the advanced clock; nothing was frozen.
	source.set(time.Date(2026, time.March, 1, 16, 30, 0, 0, time.UTC))
	issues := rejection(t, future.Check(t.Context(), at, validation.DefaultLimits())).Issues()
	if issues[0].Code != "foundry.after_now" || issues[0].Message != "This field must be in the future." {
		t.Fatalf("relative rejection: %+v", issues)
	}
	if err := validation.BeforeOrEqualNow[time.Time](service).Check(t.Context(), time.Date(2026, time.March, 1, 16, 30, 0, 0, time.UTC), validation.DefaultLimits()); err != nil {
		t.Fatal("equal instant rejected", err)
	}
	// Local wall time compares with the current wall time in the service zone.
	wall, err := temporal.ParseLocalDateTime("2026-03-02T00:15:00")
	if err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.AfterNow[temporal.LocalDateTime](service).Check(t.Context(), wall, validation.DefaultLimits()))
	if err := validation.BeforeNow[temporal.LocalDateTime](service).Check(t.Context(), wall, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}

	// Today is a calendar date in the service zone: 2026-03-02 00:30 local.
	today, err := temporal.ParseDate("2026-03-02")
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.AfterOrEqualToday[temporal.Date](service).Check(t.Context(), today, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.AfterToday[temporal.Date](service).Check(t.Context(), today, validation.DefaultLimits()))
	yesterdayUTC, err := temporal.NewDateTime(time.Date(2026, time.March, 1, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// 15:00 UTC is 23:00 on March 1 in Kuala Lumpur, so it is before today.
	if err := validation.BeforeToday[temporal.DateTime](service).Check(t.Context(), yesterdayUTC, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.BeforeOrEqualToday[temporal.Date](service).Check(t.Context(), temporal.Date{}, validation.DefaultLimits()))

	info, err := future.Description()
	if err != nil || !info.ServerOnly || info.Spec.ID != "foundry.after_now" || info.Spec.Translation == nil {
		t.Fatalf("relative metadata: %+v %v", info, err)
	}
	// A zero service is an invalid declaration, never a silent host/UTC clock.
	for _, rule := range []validation.Rule[time.Time]{validation.BeforeNow[time.Time](temporal.Service{}), validation.AfterToday[time.Time](temporal.Service{})} {
		if err := rule.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("zero temporal service accepted", err)
		}
		if err := rule.Check(t.Context(), time.Now(), validation.DefaultLimits()); err == nil {
			t.Fatal("zero temporal service checked input")
		}
	}
}

type failingClock struct{}

func (failingClock) Now() time.Time { panic("private clock failure") }

func TestRelativeTemporalClockFailureIsExecutionFailure(t *testing.T) {
	t.Parallel()
	service, err := temporal.NewService(failingClock{}, temporal.UTC)
	if err != nil {
		t.Fatal(err)
	}
	err = validation.AfterToday[time.Time](service).Check(t.Context(), time.Now(), validation.DefaultLimits())
	var rejected *validation.Errors
	if !errors.Is(err, fault.Internal) || errors.As(err, &rejected) {
		t.Fatal("clock failure was not an execution failure", err)
	}
}
