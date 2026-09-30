# Configured logging

Application logging uses structured JSON through Go's `slog`. The default is
stderr at INFO level. Select a file destination to get automatic rotation and
retention; selecting a file no longer requires a separate rotation service.

## File defaults

| Policy | Default |
| --- | --- |
| Size rollover | Before a write would exceed 20 MiB |
| Daily rollover | First write to a nonempty file on a later day in the sink timezone (default UTC) |
| Retention age | 14 days from archive creation |
| Retention count | 14 archives, plus the active file |
| New file permissions | `0600` |

Both retention limits apply, so a busy application may retain fewer than 14 days.
Existing active files are appended, never truncated. Their modification date is
used for daily rollover after restart. A record is never split across files; a
record larger than the configured size limit is rejected. Existing oversized
files roll over on the next accepted write.

Cleanup runs at startup, after each rollover, and after the first write once an
hour has passed since the last cleanup. Startup cleanup is synchronous; later
cleanups run in one worker owned by the sink, so record writes never scan the
directory. A cleanup requested while another is still pending replaces it with
the later time, so it removes everything expired by then. An idle/stopped
application does not run a cleanup timer. Archives use
the active basename followed by
`.foundry-<UTC timestamp>-<random suffix>.log`. Only regular files matching that
exact archive format for that basename are eligible for cleanup. Other files,
directories and symlinks are left alone. Age is measured from the archive name,
independent of later changes to its filesystem timestamp.

## Configuration

With configuration rooted at `application.Settings`:

```toml
time_zone = 'Asia/Kuala_Lumpur'

[log.channels.default.sink]
driver = 'file'
path = '/srv/example/logs/application.jsonl'
level = 'INFO'
# Optional per-channel override: time_zone = 'UTC'

# Optional: omitted/zero limits use the defaults above.
[log.channels.default.sink.rotation]
max_bytes = 20971520
max_files = 14
max_age = '336h'
```

Log timestamps and daily rollover inherit the [application timezone](application-timezone.md).
An explicit sink `time_zone` overrides both. Archive filenames stay UTC and
retention age remains an elapsed duration. Stacks inherit their leaf policies;
select timezone overrides on the individual sinks.

The same policy is `logging.RotationConfig` in Go. `DefaultRotationConfig()` returns
the explicit defaults. Generated `ChannelSettingsConfigKeys().Sink.Rotation`
provides typed overrides. Named channels load the same element schema from TOML
or the ordinary environment collection input; see [named configuration](named-services.md).
Each file leaf in a [logging stack](supporting-services.md#logging-and-ownership)
owns its policy. Streams/stacks reject file-only options.

Limits accept 1 byte–1 GiB, 1–1,000 archives and 1 second–365 days. Zero selects
the default, not unlimited retention. `rotation.disabled = true` explicitly
restores externally managed append-only behavior. A borrowed logger supplied
through `application.WithLogger` retains its owner's logging policy.

## Ownership and operations

Paths must be absolute and parents must already exist. Use an operator-controlled
local log directory with at most 10,000 entries. Built-in file rotation uses the
existing macOS/Linux file-lock support; network filesystems are unsupported.
Build performs no filesystem I/O. Startup owns a stable
`<filename>.foundry-rotation.lock` until shutdown closes the file. A second
rotation owner of that path fails startup; multiple instances should use separate
files or stdout/stderr collection. Do not delete the lock file or modify active
files/archives externally while the sink is running. Disable built-in rotation
when choosing an external rotation arrangement.

Writes and rollover are serialized per file leaf; cleanup runs beside them in the
sink's worker and stops before the file and lock close.

## Failure handling

Rotation and retention never drop a record:

- A failed cleanup (for example an unreadable directory or more than 10,000
  entries) is counted and retried after 1 minute, doubling up to 1 hour. Records
  keep being written. A cleanup failure at startup no longer prevents startup.
- A failed rollover keeps appending to the current file, which may then exceed
  `max_bytes`, and retries with the same backoff. Only an active file that is
  missing and cannot be reopened makes a write fail.
- A destination write failure (disk full, closed descriptor, syslog error) is
  counted. File, stdout and syslog sinks rewrite that record to stderr when it is
  at most `logging.MaxFallbackBytes` (256 KiB), so container log collection still
  receives it; larger records are counted as dropped. Stderr sinks have no
  fallback. A record can therefore appear partially in the file and fully on
  stderr. Oversized records rejected by rotation follow the same fallback.
- The first failure of each kind per sink writes one fixed-text JSON notice to
  stderr with the sink driver, a redacted `diagnostic` (Go error types and
  framework fault notes) and the OS `errno` text. It never contains error
  messages, paths from errors, or record payloads.

The sink and handler still return the destination error. Go's `slog.Logger`
methods discard handler errors, so read failure counts instead: `Sink.Stats()`
returns `logging.SinkStats` (`Records`, `Failures`, `Fallbacks`, `Dropped`,
`Queued`, `RotationFailures`, `PruneFailures`) and `ChannelSet.Stats()` returns
one `logging.ChannelStats` per owned leaf channel in name order. Stacks report
through their leaves and a borrowed default logger is not reported; a custom
handler channel reports records and failures (counted as dropped). Configured
applications with observability enabled export these counters as
`foundry_log_*{channel}` metrics (see [observability](observability.md)). A stack
offers every record to every enabled child even when one child fails.
This is application logging, not an audit ledger.

## Asynchronous sinks

Any file, stream or syslog sink can move destination writes to one owned writer:

```toml
[log.channels.default.sink.async]
enabled = true
queue = 1024          # pending records; zero selects 1,024 (maximum 65,536)
max_bytes = 4194304   # pending record bytes; zero selects 4 MiB
```

Records are copied into the bounded queue. When either bound is full the newest
record is dropped, counted in `Dropped`, reported once on stderr, and the handler
returns `fault.Overloaded`; logging never blocks the caller. A single record larger
than `max_bytes` is always dropped. Records are written
in order, and `Close` (application shutdown) drains every accepted record before
closing the destination. Async options must be absent unless `enabled` is true,
and stacks cannot select them; configure each leaf.

## Syslog

On macOS/Linux (not Windows or Plan 9) `driver = 'syslog'` writes each JSON record
to syslog using Go's `log/syslog`. Record levels map to `debug`, `info`, `warning`
and `err` severities:

```toml
[log.channels.syslog.sink]
driver = 'syslog'
level = 'INFO'

[log.channels.syslog.sink.syslog]
network = 'udp'           # udp/tcp host:port, unix/unixgram absolute path,
address = '127.0.0.1:514' # or both empty for the local daemon
tag = 'orders-api'        # optional: letters, digits, '.', '_', '-' (max 48)
facility = 'local0'       # user (default), daemon or local0-local7
```

Startup connects and fails if the daemon is unreachable. `log/syslog` reconnects
once after a failed write; later failures follow the fallback rules above. File
path and rotation options are rejected for syslog sinks.

## Custom handlers

Go code can supply any `slog.Handler` for a channel, such as a vendor or
OpenTelemetry bridge:

```go
app, err := application.New(settings,
	application.WithLogHandler("vendor", vendorHandler),
).Build(ctx)
```

The equivalent direct API is `logging.PrepareChannels(selected, settings, nil,
logging.WithHandler("vendor", vendorHandler))`. A name absent from settings is
added as a custom channel at the default INFO minimum. To choose its level from
deployment settings, configure the channel with the custom driver:

```toml
[log.channels.vendor.sink]
driver = 'custom'
level = 'WARN'
```

A custom channel accepts only `level`, which is applied before the handler's own
`Enabled`. Binding a handler to a non-custom channel, a custom channel without a
handler, a nil handler or the same name twice fails at Build. Custom channels can
be stack children. The handler is borrowed and never closed by the application,
and it receives records unchanged: wrap it with `logging.Correlate` to add the
correlation and context groups. Framework redaction applies only to the JSON
sinks, so a custom handler owns its own redaction.

Default/named loggers are `services.Logger` and `services.Logs.Channel(name)`.
Use `InfoContext`/`ErrorContext` to retain request/trace correlation. Common
credential fields are redacted, including after rotation; arbitrary nested
objects still require explicit safe logging contracts. See
[observability](observability.md) for correlation and exporters.

## Request-scoped context fields

`logging.WithAttrs(ctx, attrs...)` returns a child context whose fields are added
to every record logged with it, under the reserved `context` group, next to the
`correlation` group (`request_id`, `trace_id`, `span_id`):

```go
ctx = logging.WithAttrs(ctx, slog.String("tenant", string(tenant.ID)), slog.String("route", "orders.show"))
logger.InfoContext(ctx, "order loaded") // {"msg":"order loaded","context":{"tenant":"t1","route":"orders.show"}}
```

A later field replaces an earlier one with the same key. At most
`logging.MaxContextFields` (32) fields and one level of groups (16 members each)
are kept; keys are at most 64 bytes and strings are truncated to 1 KiB. Only
strings, integers, floats, booleans, durations and times are kept; `LogValuer`
values are resolved first, credential-like keys are redacted, and other values
(structs, maps, errors, models) are ignored so they cannot leak into every
record. `logging.AttrsFromContext(ctx)` returns an owned copy.

### Carrying correlation into jobs

Context values do not cross a queue. Snapshot them when dispatching and restore
them in the handler:

```go
type SendInvoice struct {
	InvoiceID InvoiceID              `json:"invoice_id"`
	Request   attribution.RequestID  `json:"request_id,omitempty"`
	Log       logging.ContextFields  `json:"log,omitzero"`
}

payload := SendInvoice{InvoiceID: id, Request: attribution.FromContext(ctx).Request().ID, Log: logging.FieldsFromContext(ctx)}

// In the job handler:
ctx = payload.Log.Context(ctx)
logger.InfoContext(ctx, "sending invoice", slog.String("origin_request_id", string(payload.Request)))
```

`logging.ContextFields` is a bounded JSON object; decoding reapplies every limit
and redaction rule. Durations and times travel as text. Trace context has its own
explicit propagation form: `tracing.Context` marshals as `traceparent` JSON and
`tracing.WithContext` restores it. Only put correlation identifiers and safe
scalar labels in these fields, never credentials or personal data.
