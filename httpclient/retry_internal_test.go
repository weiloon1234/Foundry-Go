package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestFullJitterStaysWithinTheExponentialCap(t *testing.T) {
	policy := RetryPolicy{Mode: SafeReads, Attempts: 5, InitialBackoff: 100 * time.Millisecond, MaxBackoff: time.Second, Jitter: true}
	varied := false
	for range 200 {
		delay := policy.delay(3)
		if delay < 0 || delay > 400*time.Millisecond {
			t.Fatal("jittered delay exceeded its cap", delay)
		}
		if delay != policy.delay(3) {
			varied = true
		}
	}
	if !varied {
		t.Fatal("full jitter produced a fixed delay")
	}
	policy.Jitter = false
	if policy.delay(3) != 400*time.Millisecond || policy.delay(9) != time.Second {
		t.Fatal("deterministic backoff changed")
	}
}

func TestRetryAfterParsesSecondsAndDates(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, test := range []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"", 0, false},
		{"7", 7 * time.Second, true},
		{" 7 ", 7 * time.Second, true},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"-1", time.Hour, true},
		{"soon", 0, false},
	} {
		got, ok := retryAfter(http.Header{"Retry-After": {test.header}}, now)
		if got != test.want || ok != test.ok {
			t.Fatalf("retryAfter(%q) = %v, %v", test.header, got, ok)
		}
	}
}
