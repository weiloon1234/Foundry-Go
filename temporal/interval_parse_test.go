package temporal_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestParseIntervalOutputStyles(t *testing.T) {
	want, _ := temporal.NewInterval(-14, 3, -(4*time.Hour + 5*time.Minute + 6*time.Second + 123456*time.Microsecond))
	for _, text := range []string{
		"-14 mons +3 days -14706123456 microseconds",
		"-1 year -2 mons +3 days -04:05:06.123456",
		"@ 1 year 2 mons -3 days 4 hours 5 mins 6.123456 secs ago",
		"-1-2 +3 -4:05:06.123456",
		"P-1Y-2M3DT-4H-5M-6.123456S",
	} {
		if got, err := temporal.ParseInterval(text); err != nil || got != want {
			t.Fatal(text, got, want, err)
		}
	}
	for _, text := range []string{"00:00:00", "@ 0", "0", "PT0S", (temporal.Interval{}).String()} {
		if got, err := temporal.ParseInterval(text); err != nil || got != (temporal.Interval{}) {
			t.Fatal(text, got, err)
		}
	}
	for _, test := range []struct {
		text         string
		months, days int32
		elapsed      time.Duration
	}{
		{"1-2", 14, 0, 0}, {"-1-2", -14, 0, 0}, {"-3 04:05:06", 0, -3, -(4*time.Hour + 5*time.Minute + 6*time.Second)},
		{"3 04:05:06", 0, 3, 4*time.Hour + 5*time.Minute + 6*time.Second},
		{"1 year 2 mons", 14, 0, 0}, {"@ 1 year 2 mons ago", -14, 0, 0},
		{"49:00:00.000001", 0, 0, 49*time.Hour + time.Microsecond}, {"PT49H0.000001S", 0, 0, 49*time.Hour + time.Microsecond},
		{"P2W3D", 0, 17, 0}, {"P1Y2M3D", 14, 3, 0},
	} {
		want, _ := temporal.NewInterval(test.months, test.days, test.elapsed)
		if got, err := temporal.ParseInterval(test.text); err != nil || got != want {
			t.Fatal(test.text, got, want, err)
		}
	}
}

func TestIntervalParsingBoundsAndSerialization(t *testing.T) {
	for _, v := range []temporal.Interval{
		temporal.Months(math.MinInt32), temporal.Months(math.MaxInt32), temporal.Days(math.MinInt32), temporal.Days(math.MaxInt32),
	} {
		got, err := temporal.ParseInterval(v.String())
		if err != nil || got != v {
			t.Fatal(got, v, err)
		}
	}
	for _, duration := range []time.Duration{time.Duration(math.MinInt64) / time.Microsecond * time.Microsecond, time.Duration(math.MaxInt64) / time.Microsecond * time.Microsecond} {
		v, _ := temporal.Elapsed(duration)
		got, err := temporal.ParseInterval(v.String())
		if err != nil || got != v {
			t.Fatal(got, v, err)
		}
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var restored temporal.Interval
		if err := json.Unmarshal(data, &restored); err != nil || restored != v {
			t.Fatal(string(data), restored, err)
		}
	}
	for _, text := range []string{"", " ", "infinity", "-infinity", "NaN", "1", "-", "P", "PT", "P1DT", "PT1.0000001S", "P1.5D", "P1D1Y", "P1M2M", "PT1H1D", "@", "@ ago", "1 day 2 days", "1 day junk", "1:60:00", "1:00:60", "1:00:00.0000001", "1:-1:00", "1--2", "1-12", "1-2 3", "2147483648 mons", "-2147483649 days", "9223372036854776 microseconds", "-9223372036854776 microseconds", strings.Repeat("1", 257)} {
		if _, err := temporal.ParseInterval(text); !errors.Is(err, fault.Invalid) {
			t.Fatal(text, err)
		}
	}
	current := temporal.Months(2)
	for _, data := range []string{`null`, `{}`, `"infinity"`} {
		if err := json.Unmarshal([]byte(data), &current); err == nil || current != temporal.Months(2) {
			t.Fatal(data, current, err)
		}
	}
}

func FuzzIntervalCanonicalRoundTrip(f *testing.F) {
	f.Add(int32(-14), int32(3), int64(-14706123456))
	f.Add(int32(math.MinInt32), int32(math.MaxInt32), int64(0))
	f.Fuzz(func(t *testing.T, months, days int32, micros int64) {
		if micros < math.MinInt64/int64(time.Microsecond) || micros > math.MaxInt64/int64(time.Microsecond) {
			t.Skip()
		}
		v, err := temporal.NewInterval(months, days, time.Duration(micros)*time.Microsecond)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := temporal.ParseInterval(v.String()); err != nil || got != v {
			t.Fatal(v, got, err)
		}
	})
}

func FuzzParseInterval(f *testing.F) {
	for _, text := range []string{"", "@ 1 year 2 mons -3 days 4 hours ago", "-1-2 +3 -04:05:06", "P-1Y2MT-6.123456S", "2147483647 mons 2147483647 days 9223372036854775 microseconds"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		v, err := temporal.ParseInterval(text)
		if err != nil {
			return
		}
		if restored, err := temporal.ParseInterval(v.String()); err != nil || restored != v {
			t.Fatal(v, restored, err)
		}
	})
}
