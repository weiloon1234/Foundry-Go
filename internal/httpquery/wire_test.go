package httpquery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

var testLimits = Limits{Bytes: 4096, Pairs: 128}

func TestParsePreservesQueryValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"", "&", "&&a=1&", "flag", "empty=", "a=1&a=2", "=empty-name",
		"name=Jane+Doe&literal=a%2Bb&percent=%252F", "a=b=c", "name=%E6%9D%8E",
		"encoded%2Fname=%3B%26%3D%00%0D%0A", "a=1&%61=2", "filter%5Bname%5D=x",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := Parse(context.Background(), raw, testLimits)
			want, nativeErr := url.ParseQuery(raw)
			if err != nil || nativeErr != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, %v; want %v, %v", got, err, want, nativeErr)
			}
		})
	}
}

func TestParseRejectsWholeMalformedQuery(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"good=1&secret=%", "secret=%0", "secret=%GG", "%GG=secret", "secret=a;b",
		"secret%FF=value", "secret=\xff", "secret=%ED%A0%80", "secret=%C0%80",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := Parse(context.Background(), raw, testLimits)
			if got != nil || !errors.Is(err, fault.Invalid) {
				t.Fatalf("partial result or missing invalid classification: %v, %v", got, err)
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "secret") {
				t.Fatal("error contains submitted name or value")
			}
		})
	}
}

func TestQueryLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		raw    string
		limits Limits
		valid  bool
	}{
		{"exact-bytes", "a=b", Limits{Bytes: 3, Pairs: 1}, true},
		{"bytes", "a=b", Limits{Bytes: 2, Pairs: 1}, false},
		{"exact-pairs", "a=1&a=2", Limits{Bytes: 7, Pairs: 2}, true},
		{"pairs", "a=1&a=2", Limits{Bytes: 7, Pairs: 1}, false},
		{"empty-slots", "&&", Limits{Bytes: 2, Pairs: 2}, false},
		{"empty-query", "", Limits{Bytes: 1, Pairs: 1}, true},
		{"no-byte-limit", "", Limits{Bytes: 0, Pairs: 1}, false},
		{"no-pair-limit", "", Limits{Bytes: 1, Pairs: 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(context.Background(), tc.raw, tc.limits)
			if (err == nil) != tc.valid || err != nil && result != nil {
				t.Fatalf("valid=%v, got %v, %v", tc.valid, result, err)
			}
		})
	}
}

func TestEncodeMatchesNativeQueryEncoding(t *testing.T) {
	t.Parallel()
	for _, values := range []url.Values{
		nil, {}, {"empty": nil}, {"empty": {}}, {"flag": {""}},
		{"z": {"3", "1"}, "a": {"Jane Doe", "+;=/&%?", "李", "\x00\r\n"}},
		{"": {"empty-name"}, "filter[name]": {"abc"}},
	} {
		got, err := Encode(context.Background(), values, testLimits)
		if want := values.Encode(); err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}

func TestEncodeRejectsBeforeReturningPartialOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		values url.Values
		limits Limits
	}{
		{"native-bytes", url.Values{"a": {strings.Repeat("x", 100)}}, Limits{Bytes: 3, Pairs: 1}},
		{"escaped-bytes", url.Values{"a": {"/"}}, Limits{Bytes: 4, Pairs: 1}},
		{"separator", url.Values{"a": {"b"}}, Limits{Bytes: 2, Pairs: 1}},
		{"after-first-value", url.Values{"a": {"b", "c"}}, Limits{Bytes: 6, Pairs: 2}},
		{"pairs", url.Values{"a": {"1", "2"}}, Limits{Bytes: 100, Pairs: 1}},
		{"omitted-keys", url.Values{"a": nil, "b": nil}, Limits{Bytes: 100, Pairs: 1}},
		{"invalid-value-utf8", url.Values{"a": {"\xff"}}, testLimits},
		{"invalid-key-utf8", url.Values{"\xff": {"a"}}, testLimits},
		{"invalid-limits", nil, Limits{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(context.Background(), tc.values, tc.limits)
			if got != "" || !errors.Is(err, fault.Invalid) {
				t.Fatalf("partial output or missing failure: %q, %v", got, err)
			}
		})
	}
	got, err := Encode(context.Background(), url.Values{"a": {"/"}}, Limits{Bytes: 5, Pairs: 1})
	if err != nil || got != "a=%2F" {
		t.Fatalf("exact byte limit: %q, %v", got, err)
	}
}

func TestQueryContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := Parse(ctx, "a=1", testLimits); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("decode cancellation: %v, %v", got, err)
	}
	if got, err := Encode(ctx, url.Values{"a": {"1"}}, testLimits); got != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("encode cancellation: %q, %v", got, err)
	}
	if got, err := Parse(nil, "", testLimits); got != nil || !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil decode context: %v, %v", got, err)
	}
	if got, err := Encode(nil, nil, testLimits); got != "" || !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil encode context: %q, %v", got, err)
	}
}

func FuzzQueryRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "a=b", "a=%2B&a=+", "key", "bad=%GG", "a=1&&a=2", "a=%00", "x=%ED%A0%80", "x=%E6%9D%8E"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := Parse(context.Background(), raw, testLimits)
		if err != nil {
			if values != nil {
				t.Fatal("partial invalid result")
			}
			return
		}
		want, err := url.ParseQuery(raw)
		if err != nil || !reflect.DeepEqual(values, want) {
			t.Fatal("disagrees with standard parser")
		}
		limits := Limits{Bytes: 16384, Pairs: testLimits.Pairs}
		encoded, err := Encode(context.Background(), values, limits)
		if err != nil || encoded != values.Encode() {
			t.Fatalf("encode: %v", err)
		}
		roundTrip, err := Parse(context.Background(), encoded, limits)
		if err != nil || !reflect.DeepEqual(values, roundTrip) {
			t.Fatalf("round trip: %v", err)
		}
	})
}

func BenchmarkQueryRejectOversizedInput(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20} {
		raw := strings.Repeat("x", size)
		b.Run(fmt.Sprintf("parse-%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = Parse(context.Background(), raw, testLimits)
			}
		})
		b.Run(fmt.Sprintf("encode-%d", size), func(b *testing.B) {
			values := url.Values{"a": {raw}}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = Encode(context.Background(), values, testLimits)
			}
		})
	}
}
