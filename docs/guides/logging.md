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

Cleanup runs at startup, at each rollover, and on the first subsequent write after
an hour since the last cleanup. An idle/stopped application does not run a cleanup
timer. Archives use the active basename followed by
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

Writes, rollover and cleanup are synchronous and serialized per file leaf.
Ordinary writes do not scan the directory; startup, rollover and hourly cleanup
do. Filesystem failures return errors from the sink/handler, and failed rollover
preserves already written data. Go's `slog.Logger` methods do not return handler
errors; callers needing direct failure handling can use the handler contract or
provide their own logger. This is application logging, not an audit ledger.

Default/named loggers are `services.Logger` and `services.Logs.Channel(name)`.
Use `InfoContext`/`ErrorContext` to retain request/trace correlation. Common
credential fields are redacted, including after rotation; arbitrary nested
objects still require explicit safe logging contracts. See
[observability](observability.md) for correlation and exporters.
