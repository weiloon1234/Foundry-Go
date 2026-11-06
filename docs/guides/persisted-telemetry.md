# Persisted telemetry

`telemetry` is an opt-in, best-effort PostgreSQL store for recent operational
history. It collects route histograms, runtime samples, backend errors, browser
reports, and security classifications. It also owns bounded reads, incident
state, flush receipts, and retention. It is not an audit log or a durable queue.

Applications own console routes and DTOs, authorization, actor attribution,
browser transport controls, source-map publication, and log-file operations.
The framework tables contain no application actor column. Existing Prometheus,
OTLP, diagnostics, and log reporting work as before when telemetry is disabled.

## Configured application ownership

`application.New` accepts `Settings.Features.Telemetry`. Enabling telemetry also
requires `Features.Observability.Enabled` and a configured PostgreSQL connection.
The builder creates the collector before the recorder, adds its reporter, and
attaches its HTTP observers and assembled route inventory. Telemetry uses its own
`http.RequestMetrics` instance; it never drains the cumulative Prometheus owner.

This example accepts settings whose database and HTTP configuration already
belong to the application. The numbers are explicit application choices, not
framework defaults. Alerts and browser admission remain disabled here.

```go
package bootstrap

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

func Build(ctx context.Context, settings application.Settings) (*application.App, error) {
	settings.Features.Observability.Enabled = true
	settings.Features.Telemetry = application.TelemetrySettings{
		Enabled: true, Schema: "operations",
		Collector: telemetry.CollectorConfig{
			Instance: "api-1", Release: "release-1",
			MaxSeries: 512, MaxEvents: 512, MaxPendingBatches: 12,
			PendingRetention: 5 * time.Minute,
		},
		Config: telemetry.Config{
			FlushInterval: 10 * time.Second,
			FlushTimeout: 5 * time.Second,
			DrainTimeout: 3 * time.Second,
			Retention: telemetry.RetentionPolicy{
				Minute: 24 * time.Hour, Hour: 7 * 24 * time.Hour,
				Issue: 7 * 24 * time.Hour, Security: 24 * time.Hour,
				MaxRuns: 1000, MaxReceiptRows: 100000,
				MaxMetricRows: 100000, MaxIssueRows: 10000, MaxEventRows: 10000,
				PruneInterval: time.Minute,
			},
			Reads: telemetry.ReadLimits{
				MaxConcurrent: 4, MaxOverviewRuns: 100, MaxOverviewAlerts: 100,
				MaxTrafficRows: 100000, MaxTrafficGroups: 512,
				MaxPage: 100, MaxPageSize: 30, Timeout: time.Second,
			},
		},
	}
	return application.New(settings).Build(ctx)
}
```

`Database` selects a named connection; blank selects the configured default.
Blank `Schema` uses that connection's primary schema, or `public` when unscoped.
An unknown explicit connection fails validation. Settings and nested slices are
snapshotted before resources start.

`app.Resources().Telemetry()` returns the engine. Inside a service factory,
`Services.Telemetry()` resolves the same instance. A disabled feature creates no
collector, reporter, observer, loop, or migration target; resolution then returns
`fault.Missing`. An externally supplied recorder through
`application.WithObservability` is rejected with enabled telemetry because the
builder cannot add its reporter to that recorder.

`Builder.TelemetryReadiness(key)` borrows a registered `*diagnostics.Runtime` and
orders telemetry after its actual provider. Without that declaration, captured
readiness is unavailable, not healthy. Telemetry does not construct diagnostics.

## Migrations and adoption

`telemetry.Migrations()` returns immutable definitions under origin
`foundry.telemetry`. They create `foundry_telemetry_controls`,
`foundry_telemetry_runs`, `foundry_telemetry_flushes`,
`foundry_telemetry_metric_buckets`, `foundry_telemetry_issues`,
`foundry_telemetry_events`, and `foundry_telemetry_alerts` in the selected schema.
Histogram, runtime, detail, and receipt payloads use the store's version 1 format.

Enabled configured applications include these definitions in `App.Migrations()`.
`Build`, `Start`, and engine construction never apply migrations. Apply them
before serving the new reader or writer through the ordinary
[`App.RunDatabaseCommand` migration path](migrations-and-seeding.md#application-migration-targets).
For example, an application's command adapter can run
`migrate up --database default --schema operations`. Direct assembly adds
`telemetry.Migrations()` to `migrate.New` and selects the same schema in the
PostgreSQL runner and `telemetry.Dependencies.Schema`.

Adoption starts with empty framework tables. Old application telemetry tables
remain a frozen archive; there is no copy, rename, bridge migration, or
legacy-format reader. Existing application control and log-operation tables
remain application-owned. Preserve historical migration definitions, checksums,
and the deployment's migration-history namespace. Application wire DTOs need
explicit mapping if their existing IDs or enums have different owners.

A rollback stops the new writer and restores the application's previous wiring.
It does not copy new telemetry back into old tables. Disabling the feature leaves
its stored rows in place. Application acknowledgement records and historical
migration definitions remain intact; rollback does not require dropping tables.

## Configuration bounds

Collector and engine policy are explicit. Zero values do not supply memory,
retention, read, or alert defaults. The validation contracts are in
[`telemetry/config.go`](../../telemetry/config.go).

| Policy | Accepted bounds |
| --- | --- |
| `MaxSeries`, `MaxEvents` | 1–4096 each |
| `MaxPendingBatches`, `PendingRetention` | 1–60 batches; 1 minute–1 hour |
| `FlushInterval` | 1 second–1 minute |
| `FlushTimeout`, `DrainTimeout`, `PruneInterval`, read `Timeout` | Positive durations |
| `Retention.Minute`, `Retention.Security` | 1 hour–7 days |
| `Retention.Hour`, `Retention.Issue` | At most 30 days; `Hour` covers `Minute`; `Issue` is at least 1 hour |
| `MaxRuns`, `MaxMetricRows` | 100–10,000 runs; 100–1,000,000 metric rows |
| `MaxIssueRows`, `MaxEventRows` | 100–10,000 issues; 100–100,000 events |
| `MaxReceiptRows` | Positive; explicitly sized for the deployment |
| Read limits | Positive; page offsets must fit; result caps cannot exceed their storage caps |
| Alert sustain/recovery | 1–60 captures each when enabled |
| Alert thresholds | Latency in `(0, 60s]`; finite ratios in `(0, 1]`; 1–1,000,000 minimum requests; 1–4096 authentication failures |

Enabled alerts require a distinct rule list and all thresholds, even when only
some rules are selected. There are no hidden alert thresholds.
`RoutePolicy` matches declared route IDs and operation names, never arbitrary
request paths. Excluded routes still produce traffic and security observations,
but are omitted from ordinary traffic summaries and HTTP incident evaluation.

Fixed limits include 512 runtime samples, 1,024 label bytes, and 20 frames.
Browser events have one quarter of `MaxEvents`; security events have one half.
These shares and the total bound apply before persistence. Small capacities can
therefore leave a source with no available share.

## Loss, receipts, and unknown outcomes

Admission and capture are in memory. Process exit can lose admitted observations,
queued batches, and counters. A successful `RecordBrowser` or reporter call is
not a persistence receipt. Overload, expired pending batches, and an incomplete
shutdown drain count as loss. Late reports after sealing also count as loss,
although those final counters may never be persisted.

The writer processes immutable batches FIFO. Cumulative bucket snapshots from
the same run replace stored values; retries never add the same snapshot twice.
Issue groups coalesce before upsert, add counts once, preserve earliest and
latest timestamps, and replace detail only for a strictly newer observation.

Each batch has a stable UUID. Its writes, incident progress, and receipt commit
in one transaction. A lost commit response leaves that same batch at the queue
head. The next attempt checks the receipt on the primary under the storage lock:

- If the receipt exists, the writer applies its confirmed reconciliation state
  once and advances the queue without repeating counts or incident transitions.
- If it is absent and the batch is still eligible, the writer attempts that same
  batch. An expired batch with a confirmed absent receipt counts as loss.
- If reconciliation itself fails, the outcome remains unresolved and the batch
  stays pending. Generic transaction-callback replay is not used.

Receipts make retries safe; they do not make the collector durable. Their
`stored_at` uses PostgreSQL time. Each writer retains receipts for
`2 * PendingRetention + max(FlushTimeout, DrainTimeout)`. That horizon must fit
within 30 days, and `Retention.Hour` must be strictly longer. A shorter-lived
pruner cannot shorten an existing receipt's recorded horizon.

Size `MaxReceiptRows` for aggregate capture rate across processes, the receipt
horizon, and bounded prune throughput. Receipt capacity rejects new batches but
still permits reconciliation of existing receipts. Ordinary storage errors and
flush-attempt timeouts increment `Runtime.StorageFailures` while the caller's
context remains live. When that context has ended, flush and prune errors count
as cancellation even if the driver reports a broken connection instead of a
context error. This accounting does not resolve an unknown transaction outcome.
Canceled flushes preserve pending work and receipt reconciliation for retry or
final drain.

## Retention and reads

Pruning runs serially in the engine loop and uses PostgreSQL time. Each pass
selects at most 500 ordinary keys per table and 50 run keys, with additional
bounds on cascaded children. Runs with live receipts remain protected. A delayed
pruner can need several passes to catch up. `Engine.Prune(ctx)` is also available
for an application-owned maintenance task; telemetry adds no separate schedule.
It never removes application acknowledgements, audit records, or log operations.

`Engine.Reader()` returns owned snapshots through these methods:

| Method | Query and result |
| --- | --- |
| `Overview` | `OverviewQuery`; retained runs, incidents, and oldest retained bucket |
| `Traffic` | `TrafficQuery`; grouped traffic and a combined summary |
| `Issues` | `IssueQuery` with a required `Source` and `orm.PageRequest`; `orm.PageResult[Issue]` |
| `Events` | `EventQuery` with a `Fingerprint` and page; `orm.PageResult[Event]` |
| `Alerts` | `AlertQuery` with an optional state and page; `orm.PageResult[Alert]` |

Reads use the primary, bounded concurrency, a timeout, and stable ordering.
An expired context on an admitted read returns `fault.Timeout` with its context cause, even when
the driver reports cancellation or a broken connection. Timed-out reads return
no partial snapshots and retain capacity until database work actually exits.
Excess concurrency returns `fault.Overloaded`. Expired rows are hidden even
before pruning. Security issues and events use `Retention.Security`; other
sources use `Retention.Issue`. Runs and their incidents use `Retention.Hour`.
`OverviewQuery.Window` is validated but does not narrow those retention windows.
A run is stale after three flush intervals without a heartbeat.

`Window.Minutes` accepts 1–43,200. Traffic uses minute buckets through 60 minutes
and hour buckets for longer windows, including the partial starting bucket
within retention. Histograms are summed before quantiles are estimated.
`QuantileEstimate` preserves unavailable and overflow states. `Truncated`
signals overview or traffic limits; it must not be presented as a complete total.

A console factory can obtain a reader from `services.Telemetry()`, then call:

```go
traffic, err := engine.Reader().Traffic(ctx, telemetry.TrafficQuery{
	Window: telemetry.Window{Minutes: 15},
})
```

Applications authorize these reads and map snapshots to their public DTOs.
`ParseFingerprint` and `ParseAlertKey` validate external identity strings.

## Incidents and application attribution

The built-in rules are `RuleLatency`, `RuleHTTPErrors`, `RulePoolPressure`,
`RuleLogFailures`, `RuleTelemetryLoss`, and `RuleAuthenticationBurst`. They use
P95 latency, HTTP status counts of at least 500, database pool usage, log-write
counter increases, telemetry loss/failure counter increases, and authentication
or rate-limit security observations respectively.

Sustain and recovery count captures, not elapsed minutes. Repeated captures of
the same cumulative minute histogram still count. Insufficient HTTP traffic
resets both progress counts without recovering an existing incident. Counter
resets do not become increases. Progress publishes only after confirmed commit,
including receipt reconciliation.

Each run/rule incident starts at episode 1. Recovery followed by sustained bad
captures reopens it with a new episode. Acknowledgement remains in force while
that episode is active; it does not suppress recovery or future episodes.

`Engine.AcknowledgeIn(ctx, tx, key)` uses the caller's actual transaction on the
engine's database, locks the incident, rejects recovered incidents, and returns
`Acknowledgement{Key, Episode}`. It never authorizes. Success inside the callback
does not promise that the outer transaction commits.

An application acknowledgement table can use `alert_key text PRIMARY KEY`,
`episode bigint NOT NULL`, its concrete actor foreign key, and an acknowledgement
timestamp. In one transaction, the application locks and rechecks the current
actor, calls `AcknowledgeIn`, and upserts attribution for the returned episode.
The [independent consumer example](../../tests/fixtures/consumer/telemetry_consumer_test.go)
shows this order with an application-owned actor repository:

```go
return db.Transaction(ctx, func(tx *database.Tx) error {
	actor, err := actors.LockAuthorized(ctx, tx, actorID)
	if err != nil {
		return err
	}
	ack, err := engine.AcknowledgeIn(ctx, tx, key)
	if err != nil {
		return err
	}
	return actors.RecordAcknowledgement(ctx, tx, telemetryActorAttribution{
		Key: ack.Key, Episode: ack.Episode, Actor: actor.ID,
	})
})
```

The repository and attribution type in that snippet belong to the example
application. Display attribution only when its episode equals the incident's
current episode. The application owns side-table retention and any audit trail.
Cross-database actor checks cannot share this atomic transaction.

## Browser admission

`Engine.RecordBrowser` accepts a `BrowserError` with `Portal`, current
`Release`, an allowlisted `BrowserCode`, and at most 20 `BrowserFrame` values.
`CollectorConfig.Browser` must enable intake and list allowed portals. Frames
name exactly `portal/_assets/basename.js`, with line 1–1,000,000 and column
0–1,000,000. URLs, encoded paths, traversal, arbitrary messages, and request
payloads are not accepted. Stored browser details are marked untrusted.

The application endpoint retains CSRF protection, a body-size limit, rate
limiting, and portal checks before calling `RecordBrowser`. Framework validation
is in-memory admission, not an HTTP security policy or delivery guarantee.
Capacity loss returns nil and increments loss accounting.

Configured applications resolve the engine through `Services.Telemetry()` and
call `RecordBrowser`; they do not retain or construct a collector. For example,
a handler's service factory can retain the resolved engine and admit a validated
transport DTO as follows:

```go
engine, err := services.Telemetry()
if err != nil {
	return err
}
return engine.RecordBrowser(ctx, telemetry.BrowserError{
	Portal: "admin", Release: "release-1", Code: telemetry.BrowserCodeTypeError,
	Frames: []telemetry.BrowserFrame{{File: "admin/_assets/app.js", Line: 1}},
})
```

The portal and release in this example must match the configured collector
policy. `Engine.RecordBrowser` delegates to that collector's existing validation,
snapshotting, and loss accounting. After `Close` begins, browser reports return
nil and count as shutdown loss without reopening intake or accessing storage.
Direct assembly can still call `Collector.RecordBrowser` on its retained
collector; both entry points have the same admission semantics.

## Private source maps

`telemetry/sourcemap` is an independent optional package. `sourcemap.New` validates
`sourcemap.Config` without filesystem I/O. It adds no uploader, route, worker, or
automatic symbolication. `Store.Read` returns owned opaque map bytes; the
application authorizes access and keeps `Directory` outside its public tree.

The build publisher writes this immutable layout:

```text
Directory/
  release-1/
    admin/
      manifest.json
      app-abc.js.map
```

The manifest at `release-1/admin/manifest.json` contains:

```json
{
  "release": "release-1",
  "portal": "admin",
  "assets": {"_assets/app-abc.js": "app-abc.js.map"}
}
```

Release and portal must match the requested directory identities. Asset keys
identify `_assets/basename` paths, and values are single filenames in the same
portal directory, never paths or `manifest.json`. Segments contain 1–128 ASCII
letters, digits, dots, underscores, or hyphens, begin with a letter or digit,
and never contain `..`. A manifest is bounded to 128 KiB and 1,023 asset entries.

`sourcemap.ParseAsset("admin/_assets/app-abc.js")` validates an exact literal
asset identity without decoding or normalizing it. `Store.Read(ctx, release,
asset)` selects only the manifest entry in a configured portal. Missing artifacts
retain `os.ErrNotExist`; unsafe or malformed artifacts fail. Artifact symlinks and
non-regular map files are rejected, and changed files fail validation. Reads
check cancellation between filesystem operations; cancellation cannot interrupt
an OS operation already in progress.

`Config.MaxBytes` is 1 byte–10 MiB; `Retention` is 1 hour–30 days; `StorageBytes`
is at least `MaxBytes` and at most 1 GiB. The config requires 1–32 distinct
portals and a borrowed clock. An empty `Directory` disables filesystem access,
but the other config fields still require valid values.

`Store.Prune` removes oldest fully manifest-owned releases to meet age and size
bounds, always preserving `Config.Release`. Age follows the newest file
modification time in the release. Unknown files or portals preserve the whole
release and are outside managed storage accounting. Inventories are bounded to
1,024 root entries, 32 portals per release, 1,024 files per portal, and 65,536 total
entries. Exceeding an inventory bound fails before deletion. An active release
that alone exceeds capacity returns `fault.Conflict` with completed progress.

Pruning can return partial progress with an error. Publishers must coordinate
with pruning; metadata checks cannot make deletion atomic with outside writers.
The application owns scheduling and recovery after a failed deletion.

## Loop and shutdown ownership

Empty `TelemetrySettings.Kernels` starts the periodic loop under HTTP, worker,
and scheduler kernels. CLI-only execution and `Start` without a selected kernel
still register cleanup but do not start a periodic loop. Explicit kernel lists
can select other known kernels. Each process captures its own memory; there is
no cluster-leader election for collection.

`Engine.Run` is single-use. Cancellation stops its loop and leaves finalization
to `Close`. Cleanup seals intake and read admission, waits for active work,
captures once more, and attempts a FIFO drain within `DrainTimeout`. Ordinary
storage and prune failures are counted and retried on later ticks rather than
terminating the application's critical task.

`Close` is idempotent; its context bounds the caller's wait. `Done` closes only
after callbacks and database work actually exit. A callback that ignores its
deadline can therefore retain ownership beyond the wait deadline. The module
waits for `Done` before database cleanup. Direct owners must preserve that order
and keep borrowed dependencies alive until then.

Telemetry never closes its database, recorder, diagnostics, logger, or clock.
It never waits for `Recorder.Done`: foundation drains reporters after resource
cleanup. Late reports count as shutdown loss and cannot reopen the queue or
write to a closed database.
