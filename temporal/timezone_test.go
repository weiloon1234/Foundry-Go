package temporal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestTimeZonesResolveExplicitLocationsAndFixedOffsets(t *testing.T) {
	t.Parallel()
	instant, err := temporal.ParseDateTime("2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		seconds int
	}{
		{"UTC", 0}, {"Asia/Kuala_Lumpur", 8 * 3600}, {"+08:00", 8 * 3600}, {"-03:30", -(3*3600 + 30*60)}, {"+23:59", 23*3600 + 59*60}, {"-00:00", 0},
	} {
		zone, err := temporal.ParseTimeZone(tc.name)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		_, offset := instant.UTC().In(zone).Zone()
		if offset != tc.seconds {
			t.Fatalf("offset for %s: %d", tc.name, offset)
		}
		local, err := instant.LocalIn(zone)
		if err != nil {
			t.Fatal(err)
		}
		again, err := local.In(zone)
		if err != nil || !again.UTC().Equal(instant.UTC()) {
			t.Fatal("timezone round trip", err)
		}
	}
	for _, input := range []string{"", "Local", " UTC", "UTC ", "+24:00", "-24:00", "+08:60", "+8:00", "+0800", "+ab:00", "Invalid/Zone", "../UTC", "/etc/localtime", "UTC\x00"} {
		zone, err := temporal.ParseTimeZone(input)
		if !errors.Is(err, fault.Invalid) || zone != nil {
			t.Fatalf("invalid zone accepted: %q", input)
		}
	}
	zone, err := temporal.ParseTimeZone("UTC")
	if err != nil || zone != time.UTC {
		t.Fatal("UTC identity was lost", err)
	}
	if zone, err := temporal.LoadTimeZone("+08:00"); zone != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("named-zone loader accepted a fixed offset", err)
	}
}
