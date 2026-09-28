package temporal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalPreservesCalendarAndElapsedComponents(t *testing.T) {
	v, err := temporal.NewInterval(-1, 31, 24*time.Hour)
	if err != nil || v.Months() != -1 || v.Days() != 31 || v.Elapsed() != 24*time.Hour || v.String() != "-1 mons +31 days +86400000000 microseconds" {
		t.Fatal(v, err)
	}
	elapsed, err := temporal.Elapsed(24 * time.Hour)
	if err != nil || elapsed == temporal.Days(1) {
		t.Fatal("calendar day conflated with elapsed hours", err)
	}
	if _, err := temporal.Elapsed(time.Nanosecond); !errors.Is(err, fault.Invalid) {
		t.Fatal("sub-microsecond precision silently lost", err)
	}
	if temporal.Months(2).Months() != 2 || (temporal.Interval{}).Elapsed() != 0 {
		t.Fatal("interval constructor/zero value changed")
	}
}
