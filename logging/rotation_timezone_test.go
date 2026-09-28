package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestDailyRotationUsesConfiguredMidnightAcrossDST(t *testing.T) {
	for _, tc := range []struct {
		zone   temporal.ZoneName
		start  string
		length time.Duration
	}{
		{"Asia/Kuala_Lumpur", "2026-09-25T00:00:00+08:00", 24 * time.Hour},
		{"America/New_York", "2026-03-08T00:00:00-05:00", 23 * time.Hour},
		{"America/New_York", "2026-11-01T00:00:00-04:00", 25 * time.Hour},
	} {
		t.Run(tc.start, func(t *testing.T) {
			zone, err := tc.zone.Location()
			if err != nil {
				t.Fatal(err)
			}
			start, err := time.Parse(time.RFC3339, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "app.log")
			f, err := openRotatingFile(path, RotationConfig{}, start, zone)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			*zone = *time.UTC // Writer owns its zone, independently of the caller.
			writeLogAt(t, f, "first", start)
			writeLogAt(t, f, "last", start.Add(tc.length-time.Nanosecond))
			if len(logArchives(t, f)) != 0 {
				t.Fatal("rotated within same local date")
			}
			writeLogAt(t, f, "new", start.Add(tc.length))
			archives := logArchives(t, f)
			if len(archives) != 1 {
				t.Fatal("did not rotate at local midnight")
			}
			data, err := f.root.ReadFile(archives[0].name)
			if err != nil || string(data) != "firstlast" {
				t.Fatal("archive content", err)
			}
		})
	}
}

func TestDailyRotationRestartUsesLocalModificationDate(t *testing.T) {
	zone, err := temporal.ZoneName("Asia/Kuala_Lumpur").Location()
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Date(2026, 9, 25, 15, 59, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("yesterday"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, previous, previous); err != nil {
		t.Fatal(err)
	}
	now := previous.Add(time.Minute)
	f, err := openRotatingFile(path, RotationConfig{}, now, zone)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeLogAt(t, f, "today", now)
	if len(logArchives(t, f)) != 1 {
		t.Fatal("restart ignored local date change")
	}
}
