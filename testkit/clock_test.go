package testkit_test

import (
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestControlledClockSupportsConcurrentAdvances(t *testing.T) {
	initial := time.Date(2026, 9, 11, 12, 0, 0, 0, time.FixedZone("MYT", 8*3600))
	source := testkit.NewClock(initial)
	var _ clock.Clock = source
	var workers sync.WaitGroup
	for range 100 {
		workers.Go(func() { source.Advance(time.Second); _ = source.Now() })
	}
	workers.Wait()
	if !source.Now().Equal(initial.Add(100*time.Second)) || source.Now().Location() != time.UTC {
		t.Fatal("clock lost advances or normalization")
	}
	source.Set(initial)
	if !source.Now().Equal(initial) {
		t.Fatal("set failed")
	}
}
