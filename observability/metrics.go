package observability

import (
	"fmt"
	"io"
	"math"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

const PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

// Collector bounds. A collector exceeding them has its remaining samples
// dropped and counted in foundry_collector_failures_total.
const (
	MaxCollectors          = 32
	MaxCollectorSamples    = 1024
	MaxMetricLabels        = 16
	MaxMetricNameBytes     = 128
	MaxMetricHelpBytes     = 256
	MaxMetricLabelBytes    = 256
	maxExpositionFamilies  = 512
	collectorFailureMetric = "foundry_collector_failures_total"
)

// processStarted is captured once when the package initializes; it is never
// modified afterwards.
var processStarted = time.Now()

// Label is one Prometheus label on a collector sample. Keep values bounded
// operational identities (connection names, roles), never user or request data.
type Label struct {
	Name  string
	Value string
}

// Collector contributes point-in-time samples to each Prometheus exposition.
// It runs synchronously during the scrape inside callback isolation: read
// in-memory statistics only, never perform I/O or wait. Retaining the writer
// after returning has no effect.
type Collector func(*MetricWriter)

type namedCollector struct {
	name    Name
	collect Collector
}

// RegisterCollector adds a named collector to later expositions. Names are
// declared semantic identifiers and unique per recorder; at most MaxCollectors
// may be registered and registration closes when the recorder closes.
func (r *Recorder) RegisterCollector(name Name, collector Collector) error {
	if r == nil || r.done == nil {
		return fault.New(fault.Invalid, "metric collection requires an initialized recorder")
	}
	if collector == nil || len(name) > 128 || !identifier.Semantic(string(name)) {
		return fault.New(fault.Invalid, "metric collector requires a declared name and callback")
	}
	r.collectorsMu.Lock()
	defer r.collectorsMu.Unlock()
	if r.closing() {
		return fault.New(fault.Closed, "metric collector registration is closed")
	}
	if len(r.collectors) >= MaxCollectors {
		return fault.New(fault.Invalid, "metric collector count exceeds its bound")
	}
	for _, existing := range r.collectors {
		if existing.name == name {
			return fault.New(fault.Duplicate, "metric collector is already registered")
		}
	}
	r.collectors = append(r.collectors, namedCollector{name: name, collect: collector})
	return nil
}

type metricFamily struct {
	name, help, kind string
	reserved         bool
	samples          []string
	keys             map[string]bool
}

// exposition accumulates families so every sample of one metric name shares a
// single HELP/TYPE block, as the text format requires.
type exposition struct {
	families map[string]*metricFamily
	order    []string
}

func (e *exposition) family(name, help, kind string, reserved bool) (*metricFamily, bool) {
	family := e.families[name]
	if family == nil {
		if len(e.families) >= maxExpositionFamilies {
			return nil, false
		}
		family = &metricFamily{name: name, help: help, kind: kind, reserved: reserved, keys: make(map[string]bool)}
		e.families[name] = family
		e.order = append(e.order, name)
		return family, true
	}
	return family, family.kind == kind && family.help == help && family.reserved == reserved
}

func (e *exposition) builtin(name, help, kind, labels string, value string) {
	family, _ := e.family(name, help, kind, true)
	family.samples = append(family.samples, name+labels+" "+value)
}

// MetricWriter validates and escapes collector samples. Invalid samples are
// dropped and counted as collector failures instead of corrupting the output.
type MetricWriter struct {
	mu      sync.Mutex
	target  *exposition
	open    bool
	samples int
	failed  bool
}

// Gauge emits a point-in-time value. Values must be finite.
func (w *MetricWriter) Gauge(name, help string, value float64, labels ...Label) {
	w.emit("gauge", name, help, value, labels)
}

// Counter emits a monotonically increasing total. Names end in "_total" and
// values are finite and non-negative.
func (w *MetricWriter) Counter(name, help string, value float64, labels ...Label) {
	if !strings.HasSuffix(name, "_total") || value < 0 {
		w.fail()
		return
	}
	w.emit("counter", name, help, value, labels)
}

func (w *MetricWriter) fail() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.failed = true
	w.mu.Unlock()
}

func (w *MetricWriter) emit(kind, name, help string, value float64, labels []Label) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return
	}
	if w.samples >= MaxCollectorSamples || math.IsNaN(value) || math.IsInf(value, 0) || !validMetricName(name) || len(help) > MaxMetricHelpBytes || !utf8.ValidString(help) || len(labels) > MaxMetricLabels {
		w.failed = true
		return
	}
	rendered, ok := renderLabels(labels)
	if !ok {
		w.failed = true
		return
	}
	family, ok := w.target.family(name, help, kind, false)
	if !ok || family.keys[rendered] {
		w.failed = true
		return
	}
	family.keys[rendered] = true
	family.samples = append(family.samples, name+rendered+" "+strconv.FormatFloat(value, 'g', -1, 64))
	w.samples++
}

func validMetricName(name string) bool {
	if name == "" || len(name) > MaxMetricNameBytes {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == ':' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validLabelName(name string) bool {
	if name == "" || len(name) > MaxMetricNameBytes || strings.HasPrefix(name, "__") {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

var labelEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
var helpEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n")

func renderLabels(labels []Label) (string, bool) {
	if len(labels) == 0 {
		return "", true
	}
	sorted := slices.Clone(labels)
	slices.SortFunc(sorted, func(a, b Label) int { return strings.Compare(a.Name, b.Name) })
	var output strings.Builder
	output.WriteByte('{')
	for i, label := range sorted {
		if !validLabelName(label.Name) || label.Name == "le" || i > 0 && sorted[i-1].Name == label.Name || len(label.Value) > MaxMetricLabelBytes || !utf8.ValidString(label.Value) {
			return "", false
		}
		if i > 0 {
			output.WriteByte(',')
		}
		output.WriteString(label.Name + "=\"" + labelEscaper.Replace(label.Value) + "\"")
	}
	output.WriteByte('}')
	return output.String(), true
}

func metricLabels(metric Metric) string {
	return "kind=\"" + labelEscaper.Replace(string(metric.Operation.Kind)) + "\",name=\"" + labelEscaper.Replace(string(metric.Operation.Name)) + "\",outcome=\"" + string(metric.Result.Outcome) + "\",status=\"" + strconv.Itoa(metric.Result.Status) + "\""
}

func formatUint(value uint64) string { return strconv.FormatUint(value, 10) }
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// WritePrometheus writes one owned snapshot in Prometheus 0.0.4 text format:
// operation metrics, recorder totals, Go runtime and process metrics, then
// samples from registered collectors. Metric labels are declared operation
// metadata; trace/request IDs, vendor state and payloads never become labels.
// Counts and cumulative histogram buckets come from the same snapshot.
// Registry and collector bounds cap the output size.
func (r *Recorder) WritePrometheus(writer io.Writer) error {
	if writer == nil {
		return io.ErrClosedPipe
	}
	snapshot := r.Snapshot()
	output := &exposition{families: make(map[string]*metricFamily)}
	for _, metric := range snapshot.Metrics {
		output.builtin("foundry_operations_total", "Completed observed operations.", "counter", "{"+metricLabels(metric)+"}", formatUint(metric.Count))
	}
	for _, metric := range snapshot.Metrics {
		labels := metricLabels(metric)
		name := "foundry_operation_duration_seconds"
		family, _ := output.family(name, "Observed operation duration in seconds.", "histogram", true)
		for i, bound := range durationBounds {
			family.samples = append(family.samples, fmt.Sprintf("%s_bucket{%s,le=\"%s\"} %d", name, labels, formatFloat(bound.Seconds()), metric.Buckets[i]))
		}
		family.samples = append(family.samples,
			fmt.Sprintf("%s_bucket{%s,le=\"+Inf\"} %d", name, labels, metric.Count),
			fmt.Sprintf("%s_sum{%s} %s", name, labels, formatFloat(metric.Seconds)),
			fmt.Sprintf("%s_count{%s} %d", name, labels, metric.Count))
	}
	if len(snapshot.Metrics) == 0 {
		output.family("foundry_operations_total", "Completed observed operations.", "counter", true)
		output.family("foundry_operation_duration_seconds", "Observed operation duration in seconds.", "histogram", true)
	}
	for _, metric := range []struct {
		name, help string
		value      uint64
	}{
		{"foundry_completed_total", "All completed observations including bounded-series overflow.", snapshot.Completed},
		{"foundry_failures_total", "Failed, timed out or panicked observations.", snapshot.Failures},
		{"foundry_dropped_series_total", "Observations exceeding the metric series bound.", snapshot.DroppedSeries},
		{"foundry_dropped_spans_total", "Rejected or invalid span observations.", snapshot.DroppedSpans},
		{"foundry_dropped_reports_total", "Error reports dropped by the bounded queue or absent runtime.", snapshot.DroppedReports},
		{"foundry_reporter_failures_total", "Failed or timed out reporter callbacks.", snapshot.ReporterFailures},
		{"foundry_dropped_traces_total", "Sampled traces dropped by the bounded queue or absent runtime.", snapshot.DroppedTraces},
		{"foundry_trace_export_failures_total", "Failed or timed out trace exporter callbacks.", snapshot.TraceExportFailures},
	} {
		output.builtin(metric.name, metric.help, "counter", "", formatUint(metric.value))
	}
	output.builtin("foundry_active_operations", "Admitted unfinished observations.", "gauge", "", strconv.Itoa(snapshot.Active))
	output.builtin("foundry_pending_error_reports", "Queued error reports.", "gauge", "", strconv.Itoa(snapshot.PendingReports))
	output.builtin("foundry_pending_traces", "Queued sampled traces.", "gauge", "", strconv.Itoa(snapshot.PendingTraces))
	writeRuntimeMetrics(output)
	writeProcessMetrics(output)
	// Reserve the failure family before collectors run so none can claim it.
	output.family(collectorFailureMetric, "Collector callbacks that failed or emitted invalid samples.", "counter", true)
	failures := snapshot.CollectorFailures
	if r != nil {
		r.collect(output)
		failures = r.collectorFailures.Load()
	}
	output.builtin(collectorFailureMetric, "Collector callbacks that failed or emitted invalid samples.", "counter", "", formatUint(failures))
	var text strings.Builder
	for _, name := range output.order {
		family := output.families[name]
		fmt.Fprintf(&text, "# HELP %s %s\n# TYPE %s %s\n", family.name, helpEscaper.Replace(family.help), family.name, family.kind)
		for _, sample := range family.samples {
			text.WriteString(sample)
			text.WriteByte('\n')
		}
	}
	rendered := text.String()
	n, err := io.WriteString(writer, rendered)
	if err == nil && n != len(rendered) {
		return io.ErrShortWrite
	}
	return err
}

func (r *Recorder) collect(output *exposition) {
	r.collectorsMu.Lock()
	collectors := slices.Clone(r.collectors)
	r.collectorsMu.Unlock()
	for _, collector := range collectors {
		writer := &MetricWriter{target: output, open: true}
		err := callback.Isolated("collect metrics", func() error { collector.collect(writer); return nil })
		writer.mu.Lock()
		writer.open = false
		failed := writer.failed
		writer.mu.Unlock()
		if err != nil || failed {
			increment(&r.collectorFailures)
		}
	}
}

// runtimeSamples uses runtime/metrics, which reads without stopping the world.
var runtimeSamples = [...]string{
	"/sched/goroutines:goroutines",
	"/sched/gomaxprocs:threads",
	"/gc/cycles/total:gc-cycles",
	"/cpu/classes/gc/pause:cpu-seconds",
	"/gc/heap/allocs:bytes",
	"/memory/classes/heap/objects:bytes",
	"/memory/classes/heap/unused:bytes",
	"/memory/classes/total:bytes",
}

func writeRuntimeMetrics(output *exposition) {
	samples := make([]metrics.Sample, len(runtimeSamples))
	for i, name := range runtimeSamples {
		samples[i].Name = name
	}
	metrics.Read(samples)
	value := func(index int) (float64, bool) {
		switch samples[index].Value.Kind() {
		case metrics.KindUint64:
			return float64(samples[index].Value.Uint64()), true
		case metrics.KindFloat64:
			return samples[index].Value.Float64(), true
		default:
			return 0, false
		}
	}
	emit := func(name, help, kind string, index int) {
		if number, ok := value(index); ok {
			output.builtin(name, help, kind, "", formatFloat(number))
		}
	}
	output.builtin("go_info", "Go runtime version.", "gauge", "{version=\""+labelEscaper.Replace(runtime.Version())+"\"}", "1")
	emit("go_goroutines", "Live goroutines.", "gauge", 0)
	emit("go_gomaxprocs", "Configured GOMAXPROCS.", "gauge", 1)
	emit("go_gc_cycles_total", "Completed garbage collection cycles.", "counter", 2)
	emit("go_gc_pause_cpu_seconds_total", "Estimated CPU time spent in stop-the-world garbage collection pauses.", "counter", 3)
	emit("go_memstats_alloc_bytes_total", "Cumulative bytes allocated on the heap.", "counter", 4)
	emit("go_memstats_heap_alloc_bytes", "Heap bytes occupied by live and unswept objects.", "gauge", 5)
	if objects, ok := value(5); ok {
		if unused, ok := value(6); ok {
			output.builtin("go_memstats_heap_inuse_bytes", "Heap bytes in in-use spans.", "gauge", "", formatFloat(objects+unused))
		}
	}
	emit("go_memstats_sys_bytes", "Bytes of memory mapped by the Go runtime.", "gauge", 7)
}

// processStats reports only values the platform provides cheaply.
type processStats struct {
	cpuSeconds, maxFDs, openFDs, residentBytes    float64
	hasCPU, hasMaxFDs, hasOpenFDs, hasResidentMem bool
}

func writeProcessMetrics(output *exposition) {
	output.builtin("process_start_time_seconds", "Process start time in seconds since the Unix epoch.", "gauge", "", formatFloat(float64(processStarted.UnixNano())/float64(time.Second)))
	output.builtin("process_uptime_seconds", "Seconds since the process started.", "gauge", "", formatFloat(time.Since(processStarted).Seconds()))
	stats := readProcessStats()
	if stats.hasCPU {
		output.builtin("process_cpu_seconds_total", "User and system CPU time consumed by the process.", "counter", "", formatFloat(stats.cpuSeconds))
	}
	if stats.hasOpenFDs {
		output.builtin("process_open_fds", "Open file descriptors.", "gauge", "", formatFloat(stats.openFDs))
	}
	if stats.hasMaxFDs {
		output.builtin("process_max_fds", "Soft file descriptor limit.", "gauge", "", formatFloat(stats.maxFDs))
	}
	if stats.hasResidentMem {
		output.builtin("process_resident_memory_bytes", "Resident memory size in bytes.", "gauge", "", formatFloat(stats.residentBytes))
	}
}
