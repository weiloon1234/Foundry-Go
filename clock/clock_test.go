package clock_test

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
)

func TestSystemClockReturnsUTCAndMicrosecondPrecision(t *testing.T) {
	var system clock.System
	for i := 0; i < 20; i++ {
		now := system.Now()
		if now.Location() != time.UTC {
			t.Fatalf("expected UTC location, got %v", now.Location())
		}
		if now.Nanosecond()%1000 != 0 {
			t.Fatalf("expected microsecond precision (nanosecond%%1000 == 0), got nanosecond %d", now.Nanosecond())
		}
	}
}
