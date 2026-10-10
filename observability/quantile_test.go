package observability

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
	"time"
)

func TestQuantilesCombineHistogramsAndExposeOverflow(t *testing.T) {
	buckets := make([]uint64, len(DurationBuckets()))
	for i := 2; i < len(buckets); i++ {
		buckets[i] = 100
	}
	p, err := EstimateQuantile(buckets, 100, .95)
	if err != nil || !p.Available || p.Overflow || p.Seconds < .010 || p.Seconds > .025 {
		t.Fatal(p, err)
	}
	overflow, _ := EstimateQuantile(make([]uint64, len(buckets)), 5, .95)
	if !overflow.Overflow || !overflow.Available {
		t.Fatal(overflow)
	}
	empty, _ := EstimateQuantile(make([]uint64, len(buckets)), 0, .95)
	if empty.Available {
		t.Fatal(empty)
	}
	buckets[0] = 101
	if _, err := EstimateQuantile(buckets, 100, .95); err == nil {
		t.Fatal("invalid histogram accepted")
	}
}
func TestCompletedSpanRetainsDeclaredRouteWithoutChangingOperation(t *testing.T) {
	reports := make(chan ErrorReport, 1)
	recorder, err := New(DefaultConfig(), func(_ context.Context, r ErrorReport) error { reports <- r; return nil })
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = recorder.Run(t.Context()) }()
	_, span, err := recorder.Start(context.Background(), Operation{Kind: HTTP, Name: "request"})
	if err != nil {
		t.Fatal(err)
	}
	span.EndWithRoute(Result{Status: 500}, fault.Diagnostic{}, attribution.Route{Method: "GET", Name: "accounts.show"})
	select {
	case report := <-reports:
		if report.Entry.Route.Name != "accounts.show" || report.Entry.Operation.Name != "request" {
			t.Fatal(report.Entry)
		}
	case <-time.After(time.Second):
		t.Fatal("missing report")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredSamplesReuseValidatedCollectors(t *testing.T) {
	r, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCollector("sample.test", func(w *MetricWriter) {
		w.Gauge("sample_test_value", "test", 7, Label{Name: "connection", Value: "primary"})
	}); err != nil {
		t.Fatal(err)
	}
	samples := r.CollectSamples()
	found := false
	for _, sample := range samples {
		if sample.Name == "sample_test_value" {
			found = sample.Value == 7 && sample.Labels == `{connection="primary"}`
		}
	}
	if !found {
		t.Fatal("structured collector sample missing")
	}
	samples[0].Name = "changed"
	if r.CollectSamples()[0].Name == "changed" {
		t.Fatal("sample aliases owner")
	}
}
