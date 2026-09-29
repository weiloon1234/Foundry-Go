package observability_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func exposition(t *testing.T, recorder *observability.Recorder) string {
	t.Helper()
	var output bytes.Buffer
	if err := recorder.WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestPrometheusIncludesRuntimeAndProcessMetrics(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := exposition(t, recorder)
	want := []string{"# TYPE go_goroutines gauge\n", "# TYPE go_gc_cycles_total counter\n", "# TYPE go_memstats_heap_alloc_bytes gauge\n", "go_memstats_heap_inuse_bytes ", "go_memstats_sys_bytes ", "go_info{version=\"" + runtime.Version() + "\"} 1\n", "process_start_time_seconds ", "process_uptime_seconds ", "# TYPE foundry_operations_total counter\n", "foundry_collector_failures_total 0\n"}
	if runtime.GOOS != "windows" {
		want = append(want, "process_cpu_seconds_total ", "process_max_fds ")
	}
	if runtime.GOOS == "linux" {
		want = append(want, "process_open_fds ", "process_resident_memory_bytes ")
	}
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Fatal("missing exposition fragment", fragment)
		}
	}
	// Every family appears once so its samples share one HELP/TYPE block.
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if name, ok := strings.CutPrefix(line, "# TYPE "); ok {
			if seen[name] {
				t.Fatal("duplicate metric family", name)
			}
			seen[name] = true
		}
	}
}

func TestCollectorsAreValidatedIsolatedAndGrouped(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		name      observability.Name
		collector observability.Collector
	}{{"", func(*observability.MetricWriter) {}}, {"Bad Name", func(*observability.MetricWriter) {}}, {"pool", nil}} {
		if err := recorder.RegisterCollector(invalid.name, invalid.collector); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid collector accepted", err)
		}
	}
	var retained *observability.MetricWriter
	var mu sync.Mutex
	if err := recorder.RegisterCollector("pool.one", func(w *observability.MetricWriter) {
		w.Gauge("app_pool_connections", "Pool connections.", 3, observability.Label{Name: "state", Value: "open"}, observability.Label{Name: "pool", Value: "one \"quoted\"\nline"})
		w.Counter("app_pool_waits_total", "Pool waits.", 7)
		mu.Lock()
		retained = w
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RegisterCollector("pool.one", func(*observability.MetricWriter) {}); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate collector accepted", err)
	}
	if err := recorder.RegisterCollector("pool.two", func(w *observability.MetricWriter) {
		w.Gauge("app_pool_connections", "Pool connections.", 4, observability.Label{Name: "state", Value: "open"}, observability.Label{Name: "pool", Value: "two"})
	}); err != nil {
		t.Fatal(err)
	}
	text := exposition(t, recorder)
	if strings.Count(text, "# TYPE app_pool_connections gauge\n") != 1 || !strings.Contains(text, `app_pool_connections{pool="one \"quoted\"\nline",state="open"} 3`) || !strings.Contains(text, `app_pool_connections{pool="two",state="open"} 4`) || !strings.Contains(text, "app_pool_waits_total 7\n") || !strings.Contains(text, "foundry_collector_failures_total 0\n") {
		t.Fatal("collector samples were not grouped and escaped", text)
	}
	// A retained writer cannot mutate later expositions.
	mu.Lock()
	retained.Gauge("app_late_metric", "Late.", 1)
	mu.Unlock()
	if strings.Contains(exposition(t, recorder), "app_late_metric") {
		t.Fatal("retained writer emitted after its collector returned")
	}
	for i, bad := range []observability.Collector{
		func(*observability.MetricWriter) { panic("private collector failure") },
		func(w *observability.MetricWriter) { w.Gauge("foundry_operations_total", "Reserved.", 1) },
		func(w *observability.MetricWriter) { w.Gauge("app_nan", "NaN.", math.NaN()) },
		func(w *observability.MetricWriter) { w.Counter("app_count", "No total suffix.", 1) },
		func(w *observability.MetricWriter) { w.Counter("app_negative_total", "Negative.", -1) },
		func(w *observability.MetricWriter) {
			w.Gauge("app_label", "Bad label.", 1, observability.Label{Name: "__reserved", Value: "x"})
		},
		func(w *observability.MetricWriter) { w.Gauge("app_pool_connections", "Different help.", 1) },
		func(w *observability.MetricWriter) {
			w.Gauge("app_duplicate", "Duplicate.", 1)
			w.Gauge("app_duplicate", "Duplicate.", 2)
		},
	} {
		if err := recorder.RegisterCollector(observability.Name(fmt.Sprintf("bad.%d", i)), bad); err != nil {
			t.Fatal(err)
		}
	}
	text = exposition(t, recorder)
	if !strings.Contains(text, "foundry_collector_failures_total 8\n") || strings.Contains(text, "private collector failure") || strings.Contains(text, "app_nan") || strings.Contains(text, "Reserved.") || strings.Count(text, "\napp_duplicate ") != 1 {
		t.Fatal("invalid collector output was not isolated and counted", text)
	}
	if recorder.Snapshot().CollectorFailures != 8 {
		t.Fatal("collector failures missing from snapshot")
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RegisterCollector("late", func(*observability.MetricWriter) {}); !errors.Is(err, fault.Closed) {
		t.Fatal("closed recorder accepted a collector", err)
	}
}
