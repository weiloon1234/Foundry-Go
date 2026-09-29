# Shared observations and trace context

The recorder owns bounded observation and trace export for one application.
The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records
acceptance and verification evidence.

Create one recorder and give it to one application:

```go
config := observability.DefaultConfig()
config.TraceExporters = []observability.TraceExporter{exportSpan}
recorder, err := observability.New(config, reportFailure)
// Handle err before using the recorder.
builder := foundation.NewBuilder(foundation.WithObservability(recorder))
```

`TraceExporter` receives a completed `observability.Entry`; `TraceBatchExporter`
receives a slice of them; `Reporter` receives an `observability.ErrorReport`. All
receive a deadline-bearing context and return an error. Construction performs no I/O. The builder claims the recorder
once, including on a failed/abandoned build. Use a fresh recorder for another
builder. Once handed to an app, let that app own `Run` and `Close`.

App startup runs the export workers before booting providers. Application
shutdown seals the maintenance gate before cancellation, waits for managed work,
then closes ordinary resources. The recorder drains last so resource cleanup
failures can be reported. Export callbacks must own their transport independently
of services that have already shut down; they receive metadata, not a resolver.
An exporter that ignores cancellation retains actual shutdown ownership. A
caller deadline does not imply that `Done` has closed or the application stopped.

Direct hosts use `Run(ctx)`, `Ready(ctx)`, `Close(ctx)` and `Done()`. Closing a
recorder that never ran does not unexpectedly invoke user callbacks: it counts
queued exports as dropped. No global logger, provider or HTTP server is installed.

## Operation contracts

`Operation` contains a typed `Kind` and declared semantic `Name`. Names are at
most 128 bytes; use bounded operation identities, never URLs, request IDs, model
IDs, SQL, error text or payload values. The built-in integrations record:

| Kind | Name | Owned operation |
| --- | --- | --- |
| Provider | provider ID | Provider boot |
| Kernel | kernel ID, or ID plus `.construct` | Construction and selected kernel run |
| Resource | qualified task/resource ID | Managed task or resource cleanup |
| HTTP | `request` | Native server handler, including response/body cleanup |
| OutboundHTTP | named client | Logical operation including retries and stream consumption |
| Job | declared job name | Reserved job execution and finalization |
| Schedule | declared schedule ID | Occurrence execution and coordination |
| Socket | `connection` | Accepted connection and owned callback/loop lifetime |
| SocketMessage | registered channel ID, otherwise `unknown` | Decoded socket operation |

Legacy foundation identifiers that cannot be semantic metric names use a stable
`declared.` prefix plus a SHA-256 digest. Public HTTP route matching and the
existing subsystem inspection remain available separately. HTTP observations
aggregate transport traffic rather than labeling individual request paths.

For domain work, use `observability.Observe(ctx, operation, callback)`. Attach a
recorder with `WithContext` outside a managed application. A missing, closed or
full recorder never blocks the domain callback; overflow is accounted for. The
callback's returned error alone decides the outcome: work that returned `nil`
stays `Succeeded` even when its context is cancelled or expires afterwards.
`Recorder.Start` on an already-cancelled context still records the span (with
trace identity) so the caller can report its `Cancelled` outcome.
Existing callback isolation remains with the owning subsystem. `Observe` closes
its span on return, panic or Goexit. Direct `Recorder.Start` users must call
`Span.End`; an unfinished span retains recorder shutdown ownership. End is
idempotent and safe for concurrent callers.

`Result` uses `Succeeded`, `Failed`, `Rejected`, `Cancelled`, `TimedOut` or
`Panicked`. HTTP additionally records the response status. Failed, timed-out and
panicked operations enter the error queue. Error classification runs inside
owned isolation because custom `Is`/`Unwrap` methods can execute application code.

Snapshots and exports contain operation metadata, timestamps, durations and typed
trace/span/request IDs. They contain no error chain, raw URL, peer IP, credentials,
subject model, body or vendor tracestate. Snapshot slices are owned copies.
Protect access to snapshots because correlation identifiers are still operational
data. `WritePrometheus` excludes correlation IDs from labels and writes a bounded
exposition using the [Prometheus text format](https://prometheus.io/docs/instrumenting/exposition_formats/).
Operation counts and cumulative histogram buckets come from one snapshot, and a
bucket never exceeds its histogram count. The authenticated diagnostics
`Metrics` endpoint serves this output.

Span admission, completion and snapshots do not share one recorder lock:
admission uses an atomic counter, series live in hash-selected shards whose
counters are atomic, the recent ring locks individual slots, and snapshots sort
after releasing every lock. Trace and span IDs come from pooled ChaCha8 streams
seeded from the operating system's cryptographic source. On an Apple M4 Max
(3 hot series, default configuration, no exporters) the parallel span benchmark
measured about 430/195/295/505 ns per span at 1/4/8/16 CPUs, compared with
605/755/1040/1190 ns before this change (`BenchmarkParallelSpans`).

## Runtime, process and collector metrics

Each exposition also includes Go runtime metrics read through `runtime/metrics`
without stopping the world: `go_info`, `go_goroutines`, `go_gomaxprocs`,
`go_gc_cycles_total`, `go_gc_pause_cpu_seconds_total`,
`go_memstats_alloc_bytes_total`, `go_memstats_heap_alloc_bytes`,
`go_memstats_heap_inuse_bytes` and `go_memstats_sys_bytes`. Process metrics are
`process_start_time_seconds` and `process_uptime_seconds` everywhere,
`process_cpu_seconds_total` and `process_max_fds` on Unix, and
`process_open_fds` and `process_resident_memory_bytes` on Linux.

`Recorder.RegisterCollector(name, collector)` adds a typed collector. It runs
synchronously during each scrape inside callback isolation, so it must only read
in-memory statistics. It emits samples through `MetricWriter.Gauge` and
`MetricWriter.Counter` with typed `Label` values:

```go
err := recorder.RegisterCollector("orders.queue", func(w *observability.MetricWriter) {
	w.Gauge("orders_queue_depth", "Queued orders.", float64(queue.Len()), observability.Label{Name: "queue", Value: "default"})
})
```

Names follow Prometheus syntax; counters end in `_total` with non-negative values;
values are finite. Collectors cannot reuse built-in families or change an
existing family's type/help, and one metric/label set appears once. At most 32
collectors with 1,024 samples, 16 labels and 256-byte label values each are
accepted. Invalid samples, panics and `Goexit` drop the offending output and
increment `foundry_collector_failures_total` (also `Snapshot.CollectorFailures`).
Registration closes with the recorder.

Configured applications register collectors automatically when observability is
enabled. Named databases export `foundry_database_connections{connection,role,state}`,
`foundry_database_max_open_connections`, `foundry_database_waits_total`,
`foundry_database_wait_seconds_total` and `foundry_database_ready`. Named Redis
connections export `foundry_redis_connections{connection,state}`,
`foundry_redis_active_operations`, `foundry_redis_subscriptions`,
`foundry_redis_subscription_connections` and `foundry_redis_ready`. An enabled
realtime hub exports `foundry_realtime_connections`, `_subscriptions`,
`_active_operations`, `_background_tasks`, `_degraded`, `_stopping`,
`_streaming`, `_queued_bytes` and the accepted/rejected/publication/
slow-consumer/failure/stream-gap/resubscription/dropped-envelope/
operation-overload `_total` counters. Owned log channels (not an injected
default logger) export `foundry_log_records_total{channel}`,
`foundry_log_write_failures_total`, `foundry_log_fallbacks_total`,
`foundry_log_dropped_total`, `foundry_log_rotation_failures_total`,
`foundry_log_retention_failures_total` and `foundry_log_queued_records`. Job
connections export `foundry_jobs_queue_jobs{connection,queue,state}` (state
`waiting`, `delayed`, `blocked`, `leased`, `failed` or `retained`) for each
connection's default queue and `foundry_jobs_queue_sampled_timestamp_seconds`;
a background `jobs.DepthMonitor` samples `Dispatcher.Stats` every 15 seconds, so
scrapes never call the queue authority, and backends without statistics are
omitted. Labels are configured connection, pool role, queue and channel names
only.

`logging.JSON` adds a reserved `correlation` group from each `InfoContext` /
`ErrorContext` call's context. It includes request, trace and span IDs when present
and never retains that context for the next call. Custom loggers can explicitly
wrap a handler with `logging.Correlate`. Existing logger attributes/groups are
preserved. Keep application attributes outside the reserved group. Common
credential keys and raw `tracestate` fields are redacted; `tracing.Context` also
implements a safe structured log value that omits vendor state. Arbitrary nested
application objects still need explicit safe logging contracts.

## Bounded exports and sampling

| Setting | Default | Maximum |
| --- | --- | --- |
| Metric series | 512 | 4,096 |
| Recent entries | 128 | 4,096; zero disables retention |
| Active spans | 65,536 | 1,048,576 |
| Error queue | 128 | 4,096 |
| Error workers | 2 | 16 |
| Trace queue | 512 | 4,096 |
| Trace workers | 2 | 16 |
| Export callback deadline | 3 seconds | 1 minute |
| Each exporter family | explicit callbacks | 16 callbacks |

Queue overflow drops export metadata without blocking application work. Trace
traffic cannot evict error reports. Global totals and drop/failure counters remain
available even after the series bound is reached. Each worker runs callbacks in
declaration order with separate isolation/deadlines. Different workers may export
entries out of order. Exporters must be concurrency-safe; callbacks that ignore
their deadlines occupy their worker until actual return.

`SampleTraces` is an explicit local boolean policy, enabled by default. It controls
the outgoing sampled flag and whether completed spans enter the trace queue.
`TraceSampleRatio` (application setting `trace_sample_ratio`) then samples that
fraction of traces, deterministically from the random low 64 bits of the trace
ID, so every span of one trace, and every process using the same ratio, agrees.
Zero selects 1 (every trace); disable sampling with `SampleTraces = false`.
`tracing.RatioSampler` and `tracing.StartSampled` expose the same policy.
Metrics, error reports and bounded recent metadata remain available when sampling
is disabled. Incoming sampling flags never force the local export policy.

`TraceBatchExporters` receive up to `TraceBatchSize` (default 64, at most 512)
entries per call. A trace worker takes one queued entry, then only entries
already queued, so light traffic exports promptly in small batches. Each call
receives an owned slice. Per-entry and batch exporters together are limited to
16; one failed batch call counts one export failure.

## OTLP export

`observability.NewOTLPExporter` returns a standard-library OTLP/HTTP JSON batch
exporter. Configured applications add it with `application.WithTraceBatchExporter`:

```go
exporter, err := observability.NewOTLPExporter(observability.OTLPConfig{
	Endpoint:    "https://collector.example:4318/v1/traces",
	ServiceName: "orders",
	Headers:     []observability.OTLPHeader{{Name: "Authorization", Value: secret.New(token)}},
})
// Handle err, then pass application.WithTraceBatchExporter(exporter).
```

`Endpoint` is the complete absolute HTTP(S) traces URL without credentials. Header
values are `secret.String`; transport-owned headers are rejected. The default
client never follows redirects, so headers reach only the endpoint; `Timeout`
(default 10 seconds, at most a minute) and the recorder's `TraceTimeout` bound
each call. Spans carry the operation kind/name, outcome, HTTP status, request ID,
trace/span/parent IDs and timing, with OTLP span kinds server (HTTP/socket),
client (outbound HTTP), consumer (job) or internal, and error status for failed,
panicked or timed-out operations. There are no payloads, error text or vendor
tracestate. Each batch is attempted once; a non-2xx response or transport error
counts as an export failure without retry.

## Propagation boundaries

The immutable `tracing.Context` implements bounded
[W3C trace context](https://www.w3.org/TR/trace-context/) parsing and formatting.
Trace and span IDs have different Go types. Roots and children use
cryptographically strong randomness (pooled ChaCha8 streams seeded from the
operating system). `tracing.Start` also derives identity for an already-cancelled
context; cancellation stays with the caller. `Parse` rejects invalid parents and discards invalid vendor state
independently. Explicit JSON snapshots validate vendor state strictly. No trace
metadata grants authentication or authorization.

Every native HTTP request clears the long-lived kernel trace. Default server
configuration creates an independent request trace. Set `TrustTraceContext` only
for an ingress where propagating caller-supplied correlation is intended. Valid
single parents then produce child spans; malformed/duplicate parents start fresh
traces. Foundry still generates its own `X-Request-ID`.

Named outbound HTTP clients add no trace headers by default. Set
`httpclient.Config.PropagateTrace` for destinations that should receive them.
The owned native request receives the current trace, replacing manual trace
headers without changing the reusable request declaration. Header budgets still
apply after propagation. A logical retry sequence shares its operation span;
reusing a request in another operation gets another span. No automatic redirect
or extra retry policy is introduced.

Workers and schedulers continue discarding unrelated kernel context values. A
legacy job gets a fresh trace correlated with its captured request ID. Schedules
get an occurrence trace; nested domain/client work becomes a child. A socket
copies only its explicit recorder, trace and existing request attribution into
the hub-owned connection context. Message spans descend from that connection.
The old HTTP deadline is not treated as failure after a successful hijack; the
socket owner controls its own timeout and shutdown.

For persistent job parent propagation, use `jobs.Options[P]{PropagateTrace:true}`
after the [workers-first envelope rollout](job-trace-rollout.md). No live context
or exporter is retained in a queued message. See the
[independent consumer](../../tests/fixtures/consumer/tooling/production.go) and
[protected diagnostics](production-diagnostics.md).

## Error reports in structured logs

`observability.LogReporter(logger, config)` is a built-in `Reporter` that writes
one `operation failed` record per accepted report with the operation kind/name,
outcome, HTTP status, duration, trace/span/request IDs, a short fingerprint and
the redacted `Diagnostic`. It never logs error text or payloads.
`DefaultLogReporterConfig()` logs at ERROR and suppresses reports sharing a
fingerprint (operation, outcome, status and diagnostic types/faults/attributes/
frames, never IDs or times) for one minute; the first report after the window
carries a `suppressed` count. `Window` zero logs every report. At most
`MaxFingerprints` (default 256) fingerprints are retained, forgetting the least
recently reported. `DontReportFaults`, `DontReportStatuses` and
`DontReportOutcomes` skip matching reports; fault codes match any framework fault
recorded in the diagnostic.

Configured applications enable it on the default logger with
`features.observability.error_log.enabled = true`, plus optional `window`,
`max_fingerprints`, `dont_report_faults` and `dont_report_statuses`. A supplied
recorder owns its reporters, so combining it with the setting is rejected.

A third-party service is another `Reporter`. For example, a Sentry-style
reporter maps the same safe metadata to an event without a new framework
dependency:

```go
func sentryReporter(client *sentry.Client) observability.Reporter {
	return func(ctx context.Context, report observability.ErrorReport) error {
		event := sentry.NewEvent()
		event.Message = string(report.Entry.Operation.Kind) + " " + string(report.Entry.Operation.Name) + " " + string(report.Entry.Result.Outcome)
		event.Fingerprint = []string{string(report.Entry.Operation.Name), strings.Join(report.Diagnostic.Types, ",")}
		event.Tags = map[string]string{"trace_id": report.Entry.TraceID.String(), "request_id": string(report.Entry.RequestID)}
		for _, frame := range report.Diagnostic.Frames {
			event.Extra[frame.Function] = frame.File + ":" + strconv.Itoa(frame.Line)
		}
		return client.CaptureEvent(ctx, event)
	}
}
```

Register it with `application.WithErrorReporter`. The callback runs in a bounded
worker with the recorder's `ReporterTimeout`; it must honor cancellation.

## Error classification bounds

`observability.OutcomeFor` preserves wrapped/joined error classification without
formatting domain error text. Cyclic or excessively large/deep error graphs are
classified as failed operations, so reporting can release the operation span.
Custom `Is`/`Unwrap` methods still must return, and `Is` must compare shallowly;
panics and `runtime.Goexit` remain isolated and classified as callback failures.
