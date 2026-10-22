package temporal_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestUTCNormalizationAndCalendarArithmetic(t *testing.T) {
	v, err := temporal.ParseDateTime("2026-04-11T13:00:00.123+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "2026-04-11T05:00:00.123Z" {
		t.Fatal(v.String())
	}
	same, err := temporal.NewDateTime(v.UTC())
	if err != nil || same != v {
		t.Fatal("normalization not comparable")
	}
	next, err := v.Add(time.Hour)
	if err != nil || next.UTC().Sub(v.UTC()) != time.Hour || v.String() != "2026-04-11T05:00:00.123Z" {
		t.Fatal("instant mutated")
	}
	date, err := temporal.ParseDate("2024-02-28")
	if err != nil {
		t.Fatal(err)
	}
	leap, err := date.AddDays(1)
	if err != nil || leap.String() != "2024-02-29" {
		t.Fatal("leap date arithmetic failed")
	}
	local, err := temporal.ParseLocalDateTime("2024-02-29 23:59:59.5")
	if err != nil {
		t.Fatal(err)
	}
	after, err := local.Add(time.Second)
	if err != nil || after.String() != "2024-03-01T00:00:00.5" || local.Date() != leap {
		t.Fatal("local calendar arithmetic failed")
	}
	if _, err := date.AddDays(int(^uint(0) >> 1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("overflow accepted")
	}
}

func TestDSTGapFoldAndExplicitZones(t *testing.T) {
	for _, test := range []struct {
		zone, input, utc string
		code             fault.Code
	}{
		{"Asia/Kuala_Lumpur", "2026-04-11T13:00:00", "2026-04-11T05:00:00Z", ""},
		{"America/New_York", "2026-03-08T02:30:00", "", fault.Invalid},
		{"America/New_York", "2026-11-01T01:30:00", "", fault.Conflict},
		{"America/New_York", "2026-11-01T03:30:00", "2026-11-01T08:30:00Z", ""},
		{"Australia/Lord_Howe", "2026-04-05T01:45:00", "", fault.Conflict},
		{"Pacific/Apia", "2011-12-30T12:00:00", "", fault.Invalid},
	} {
		t.Run(test.zone+test.input, func(t *testing.T) {
			zone, err := time.LoadLocation(test.zone)
			if err != nil {
				t.Fatal(err)
			}
			local, err := temporal.ParseLocalDateTime(test.input)
			if err != nil {
				t.Fatal(err)
			}
			instant, err := local.In(zone)
			if test.code != "" {
				if !errors.Is(err, test.code) {
					t.Fatalf("expected %s: %v", test.code, err)
				}
				return
			}
			if err != nil || instant.String() != test.utc {
				t.Fatalf("%s: %v", instant, err)
			}
			roundTrip, err := instant.LocalIn(zone)
			if err != nil || roundTrip != local {
				t.Fatal("timezone round trip failed")
			}
		})
	}
}

func TestTemporalJSONRoundTripsAndRejectsNull(t *testing.T) {
	type record struct {
		Date    temporal.Date
		Time    temporal.Time
		Instant temporal.DateTime
		Local   temporal.LocalDateTime
	}
	var value record
	input := `{"Date":"2026-09-11","Time":"00:00:00","Instant":"2026-09-11T00:00:00Z","Local":"2026-09-11T00:00:00"}`
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) != input {
		t.Fatalf("%s: %v", data, err)
	}
	for _, field := range []string{"Date", "Time", "Instant", "Local"} {
		copy := value
		if err := json.Unmarshal([]byte(`{"`+field+`":null}`), &copy); !errors.Is(err, fault.Invalid) || copy != value {
			t.Fatal("null accepted or mutated value")
		}
	}
	if _, err := json.Marshal(temporal.Date{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("absent date serialized")
	}
	if _, err := json.Marshal(temporal.LocalDateTime{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("absent local date-time serialized")
	}
}

func TestInvalidTemporalBoundaries(t *testing.T) {
	for _, input := range []string{"2026-02-29", "0000-01-01", "2026-1-01", "10000-01-01"} {
		if _, err := temporal.ParseDate(input); err == nil {
			t.Fatalf("accepted date %s", input)
		}
	}
	for _, input := range []string{"24:00:00", "12:60:00", "23:59:60", "12:00:00.1234567890", "12:00:00Z", "1:00:00"} {
		if _, err := temporal.ParseTime(input); err == nil {
			t.Fatalf("accepted time %s", input)
		}
	}
	for _, input := range []string{"2026-01-01T00:00:00", "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+01:60", "2026-01-01T0:00:00Z", "2026-01-01T00:00:00.1234567890Z", "0000-12-31T23:00:00-01:00"} {
		if _, err := temporal.ParseDateTime(input); err == nil {
			t.Fatalf("accepted instant %s", input)
		}
	}
	local, _ := temporal.ParseLocalDateTime("2026-01-01T00:00:00")
	if _, err := local.In(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil zone accepted")
	}
	if _, err := local.In(time.FixedZone("unsupported", 25*3600)); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported offset accepted")
	}
}

func TestTemporalMicrosecondNormalization(t *testing.T) {
	// NewDateTime with a nanosecond instant truncates (no rounding up: ...999999999 -> ...999999000).
	instantWithNs := time.Date(2026, 10, 22, 10, 0, 0, 999999999, time.UTC)
	dt, err := temporal.NewDateTime(instantWithNs)
	if err != nil {
		t.Fatal(err)
	}
	if dt.UTC().Nanosecond() != 999999000 {
		t.Fatalf("expected nanosecond 999999000, got %d", dt.UTC().Nanosecond())
	}
	if dt.UTC().Nanosecond()%1000 != 0 {
		t.Fatalf("sub-microsecond precision remained: %d", dt.UTC().Nanosecond())
	}

	// ParseDateTime with nanoseconds yields .123456.
	parsed, err := temporal.ParseDateTime("2026-10-22T10:00:00.123456789Z")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.UTC().Nanosecond() != 123456000 {
		t.Fatalf("expected nanosecond 123456000, got %d", parsed.UTC().Nanosecond())
	}
	if parsed.String() != "2026-10-22T10:00:00.123456Z" {
		t.Fatalf("expected 2026-10-22T10:00:00.123456Z, got %s", parsed.String())
	}

	// DateTime.Add(1ns) stays normalized.
	added, err := dt.Add(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if added.UTC().Nanosecond()%1000 != 0 {
		t.Fatalf("sub-microsecond precision remained after Add: %d", added.UTC().Nanosecond())
	}
	if added != dt {
		t.Fatalf("expected 1ns Add to truncate to unchanged microsecond instant, got %v vs %v", added, dt)
	}

	// WallTime (Time): NewTime truncates without rounding up.
	tm, err := temporal.NewTime(10, 0, 0, 999999999)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Nanosecond() != 999999000 || tm.Nanosecond()%1000 != 0 {
		t.Fatalf("expected Time nanosecond 999999000, got %d", tm.Nanosecond())
	}

	// ParseTime with nanoseconds yields .123456.
	parsedTime, err := temporal.ParseTime("10:00:00.123456789")
	if err != nil {
		t.Fatal(err)
	}
	if parsedTime.Nanosecond() != 123456000 || parsedTime.Nanosecond()%1000 != 0 {
		t.Fatalf("expected Time nanosecond 123456000, got %d", parsedTime.Nanosecond())
	}
	if parsedTime.String() != "10:00:00.123456" {
		t.Fatalf("expected 10:00:00.123456, got %s", parsedTime.String())
	}

	// LocalDateTime: ParseLocalDateTime truncates to microsecond precision.
	parsedLocal, err := temporal.ParseLocalDateTime("2026-10-22T10:00:00.123456789")
	if err != nil {
		t.Fatal(err)
	}
	if parsedLocal.Time().Nanosecond() != 123456000 || parsedLocal.Time().Nanosecond()%1000 != 0 {
		t.Fatalf("expected local date-time nanosecond 123456000, got %d", parsedLocal.Time().Nanosecond())
	}
	if parsedLocal.String() != "2026-10-22T10:00:00.123456" {
		t.Fatalf("expected 2026-10-22T10:00:00.123456, got %s", parsedLocal.String())
	}

	// LocalDateTime.Add(1ns) stays normalized.
	addedLocal, err := parsedLocal.Add(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if addedLocal.Time().Nanosecond()%1000 != 0 {
		t.Fatalf("sub-microsecond precision remained after local Add: %d", addedLocal.Time().Nanosecond())
	}
	if addedLocal != parsedLocal {
		t.Fatalf("expected 1ns Add to truncate to unchanged local date-time, got %v vs %v", addedLocal, parsedLocal)
	}
}

func FuzzLocalDateTimeRoundTrip(f *testing.F) {
	f.Add("2026-09-11T12:15:00.123")
	f.Fuzz(func(t *testing.T, input string) {
		v, err := temporal.ParseLocalDateTime(input)
		if err != nil {
			return
		}
		parsed, err := temporal.ParseLocalDateTime(v.String())
		if err != nil || parsed != v {
			t.Fatal("round trip failed")
		}
	})
}
