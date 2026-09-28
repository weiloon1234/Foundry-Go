package observability

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

const PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

func metricLabels(metric Metric) string {
	escape := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
	return "kind=\"" + escape.Replace(string(metric.Operation.Kind)) + "\",name=\"" + escape.Replace(string(metric.Operation.Name)) + "\",outcome=\"" + string(metric.Result.Outcome) + "\",status=\"" + strconv.Itoa(metric.Result.Status) + "\""
}

// WritePrometheus writes one owned snapshot in Prometheus 0.0.4 text format.
// Metric labels are declared operation metadata; trace/request IDs, vendor
// state and payloads never become labels. Counts and cumulative histogram
// buckets come from the same snapshot. Registry bounds cap the output size.
func (r *Recorder) WritePrometheus(writer io.Writer) error {
	if writer == nil {
		return io.ErrClosedPipe
	}
	snapshot := r.Snapshot()
	var output strings.Builder
	fmt.Fprintln(&output, "# HELP foundry_operations_total Completed observed operations.")
	fmt.Fprintln(&output, "# TYPE foundry_operations_total counter")
	for _, metric := range snapshot.Metrics {
		fmt.Fprintf(&output, "foundry_operations_total{%s} %d\n", metricLabels(metric), metric.Count)
	}
	fmt.Fprintln(&output, "# HELP foundry_operation_duration_seconds Observed operation duration in seconds.")
	fmt.Fprintln(&output, "# TYPE foundry_operation_duration_seconds histogram")
	for _, metric := range snapshot.Metrics {
		labels := metricLabels(metric)
		for i, bound := range durationBounds {
			fmt.Fprintf(&output, "foundry_operation_duration_seconds_bucket{%s,le=\"%s\"} %d\n", labels, strconv.FormatFloat(bound.Seconds(), 'g', -1, 64), metric.Buckets[i])
		}
		fmt.Fprintf(&output, "foundry_operation_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, metric.Count)
		fmt.Fprintf(&output, "foundry_operation_duration_seconds_sum{%s} %s\n", labels, strconv.FormatFloat(metric.Seconds, 'g', -1, 64))
		fmt.Fprintf(&output, "foundry_operation_duration_seconds_count{%s} %d\n", labels, metric.Count)
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
		fmt.Fprintf(&output, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", metric.name, metric.help, metric.name, metric.name, metric.value)
	}
	fmt.Fprintf(&output, "# HELP foundry_active_operations Admitted unfinished observations.\n# TYPE foundry_active_operations gauge\nfoundry_active_operations %d\n", snapshot.Active)
	fmt.Fprintf(&output, "# HELP foundry_pending_error_reports Queued error reports.\n# TYPE foundry_pending_error_reports gauge\nfoundry_pending_error_reports %d\n", snapshot.PendingReports)
	fmt.Fprintf(&output, "# HELP foundry_pending_traces Queued sampled traces.\n# TYPE foundry_pending_traces gauge\nfoundry_pending_traces %d\n", snapshot.PendingTraces)
	text := output.String()
	n, err := io.WriteString(writer, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}
