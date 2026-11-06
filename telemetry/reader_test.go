package telemetry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestReaderValidatesInputsBeforeDatabaseAdmission(t *testing.T) {
	r := &Reader{config: engineConfig()}
	for _, minutes := range []int{-1, 0, 43201, math.MaxInt} {
		if _, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: minutes}}); !errors.Is(err, fault.Invalid) {
			t.Fatalf("overview accepted window %d", minutes)
		}
		if _, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: minutes}}); !errors.Is(err, fault.Invalid) {
			t.Fatalf("traffic accepted window %d", minutes)
		}
	}
	for _, minutes := range []int{1, 60, 61, 43200} {
		if err := (Window{Minutes: minutes}).Validate(); err != nil {
			t.Fatalf("valid window %d: %v", minutes, err)
		}
	}
	for _, page := range []orm.PageRequest{orm.Page(0, 1), orm.Page(1, 0), orm.Page(r.config.Reads.MaxPage+1, 1), orm.Page(1, r.config.Reads.MaxPageSize+1), orm.Page(math.MaxInt, math.MaxInt)} {
		if _, err := r.Issues(t.Context(), IssueQuery{Source: SourceBackend, Page: page}); !errors.Is(err, fault.Invalid) {
			t.Fatal("issue page bypassed configured bounds")
		}
		if _, err := r.Events(t.Context(), EventQuery{Fingerprint: Fingerprint(digest("valid")), Page: page}); !errors.Is(err, fault.Invalid) {
			t.Fatal("event page bypassed configured bounds")
		}
		if _, err := r.Alerts(t.Context(), AlertQuery{Page: page}); !errors.Is(err, fault.Invalid) {
			t.Fatal("alert page bypassed configured bounds")
		}
	}
	if _, err := r.Issues(t.Context(), IssueQuery{Source: Source("unknown"), Page: orm.Page(1, 1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unvalidated source reached storage")
	}
	if _, err := r.Issue(t.Context(), "arbitrary"); !errors.Is(err, fault.Invalid) {
		t.Fatal("unvalidated single issue fingerprint reached storage")
	}
	if _, err := r.Events(t.Context(), EventQuery{Fingerprint: "arbitrary", Page: orm.Page(1, 1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unvalidated fingerprint reached storage")
	}
	if _, err := r.Alerts(t.Context(), AlertQuery{State: value.Set(AlertState("unknown")), Page: orm.Page(1, 1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unvalidated state reached storage")
	}
	if _, err := (&Reader{}).Overview(context.Background(), OverviewQuery{Window: Window{Minutes: 1}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero reader did not reject construction misuse")
	}
}

func TestReaderTrafficWindowIncludesPartialBucketWithoutExtendingRetention(t *testing.T) {
	now, err := temporal.NewDateTime(time.Date(2026, 11, 6, 12, 34, 56, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	policy := retentionConfig().Retention
	for _, test := range []struct {
		minutes  int
		interval time.Duration
		want     time.Time
	}{
		{15, time.Minute, time.Date(2026, 11, 6, 12, 19, 0, 0, time.UTC)},
		{60, time.Minute, now.UTC().Add(-policy.Minute)},
		{61, time.Hour, time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC)},
		{43200, time.Hour, now.UTC().Add(-policy.Hour)},
	} {
		interval, since, err := trafficWindow(now, Window{Minutes: test.minutes}, policy)
		if err != nil || interval != test.interval || !since.UTC().Equal(test.want) {
			t.Fatalf("window %d: interval=%s since=%v err=%v", test.minutes, interval, since, err)
		}
	}
}

func TestReaderHistogramRejectsArithmeticCorruptionAndPreservesUnavailable(t *testing.T) {
	for _, test := range []struct {
		name        string
		first, next store.Histogram
	}{
		{"count overflow", readerHistogram(math.MaxUint64, 0, true), readerHistogram(1, 0, true)},
		{"duration overflow", readerHistogram(1, math.MaxFloat64, true), readerHistogram(1, math.MaxFloat64, true)},
		{"nonfinite duration", store.Histogram{}, readerHistogram(1, math.NaN(), true)},
		{"empty duration", store.Histogram{}, readerHistogram(0, 1, true)},
		{"incompatible buckets", store.Histogram{}, store.Histogram{Count: 1, Buckets: []uint64{1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := addReadHistogram(&test.first, test.next); !errors.Is(err, fault.Internal) {
				t.Fatal("corrupt aggregate was not classified as storage failure")
			}
		})
	}
	zero := readerHistogram(0, 0, false)
	estimate, err := readQuantile(zero, .95)
	if err != nil || estimate.Available || estimate.Overflow {
		t.Fatal("empty histogram fabricated an estimate")
	}
	bad := readerHistogram(2, .1, false)
	bad.Buckets[1] = 1
	if _, err := readQuantile(bad, .95); !errors.Is(err, fault.Internal) {
		t.Fatal("non-monotonic persisted histogram escaped storage validation")
	}
}
