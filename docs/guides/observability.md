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

`TraceExporter` receives a completed `observability.Entry`; `Reporter` receives
an `observability.ErrorReport`. Both receive a deadline-bearing context and
return an error. Construction performs no I/O. The builder claims the recorder
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
full recorder never blocks the domain callback; overflow is accounted for.
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
data. `WritePrometheus` excludes correlation IDs from labels and writes a single
consistent bounded snapshot using the [Prometheus text format](https://prometheus.io/docs/instrumenting/exposition_formats/).

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
Metrics, error reports and bounded recent metadata remain available when sampling
is disabled. Incoming sampling flags never force the local export policy.

## Propagation boundaries

The immutable `tracing.Context` implements bounded
[W3C trace context](https://www.w3.org/TR/trace-context/) parsing and formatting.
Trace and span IDs have different Go types. Roots and children use cryptographic
randomness. `Parse` rejects invalid parents and discards invalid vendor state
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

## Error classification bounds

`observability.OutcomeFor` preserves wrapped/joined error classification without
formatting domain error text. Cyclic or excessively large/deep error graphs are
classified as failed operations, so reporting can release the operation span.
Custom `Is`/`Unwrap` methods still must return, and `Is` must compare shallowly;
panics and `runtime.Goexit` remain isolated and classified as callback failures.
