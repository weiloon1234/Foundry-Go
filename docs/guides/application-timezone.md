# Application timezone and date helpers

Set one application timezone; UTC is the default. The setting is owned by each
application, so independent applications in the same process remain isolated.

```go
settings := application.DefaultSettings()
settings.TimeZone = "Asia/Kuala_Lumpur" // temporal.ZoneName
```

For configuration rooted at `application.Settings`:

```toml
time_zone = 'Asia/Kuala_Lumpur'
```

With schema prefix `APP`, the environment key is `APP__TIME_ZONE`.
`application.SettingsConfigKeys().TimeZone.Set(temporal.UTC)` is the typed override.
When embedding settings under `App`, use `app.time_zone` and the corresponding
nested generated key. Supported values are IANA names, `UTC` and fixed offsets
such as `+08:00`; `Local` and unknown names fail Build before acquiring resources.
An empty application setting selects UTC. Standard Go timezone data is used with
an embedded IANA fallback for minimal images; updates follow the Go toolchain.

## Retain a concrete date service

Construct domain services with `s.Time()` and retain the returned `temporal.Service`.
It borrows the application's injected clock, including `application.WithClock`
test clocks, and binds calendar/presentation operations to the selected timezone.
No request context or repeated service-container forwarding is needed.

```go
type Reports struct { dates temporal.Service }

func NewReports(dates temporal.Service) *Reports {
    return &Reports{dates: dates}
}

// In an application declaration constructor:
reports := NewReports(s.Time())
```

The following operations return a value and an error. Handle each error normally:

```go
dates := s.Time()
now, err := dates.Now()                    // UTC instant
today, err := dates.Today()                // date in the application zone
when, err := dates.Parse("2026-09-26T09:00:00")
text, err := dates.Format(when)            // 2026-09-26T09:00:00+08:00
start, err := dates.StartOfDay(when)       // local midnight as a UTC instant
tomorrow, err := dates.AddDays(when, 1)    // same local time next calendar day
```

`Date(instant)` and `Local(instant)` extract typed calendar values. `Location()`
returns an owned `*time.Location` for standard Go APIs. `TimeZone()` returns the
typed name. `dates.In("America/New_York")` returns a new service using the same
clock, without modifying the original. Direct composition uses
`temporal.NewService(clock, zone)`.

Parsing honors an explicit RFC3339 offset before using the default zone. Local
times in DST gaps fail with `fault.Invalid`; repeated local times fail with
`fault.Conflict`. Supply an offset to disambiguate. `AddDays` preserves wall time,
so a day may be 23 or 25 elapsed hours; it also rejects resulting gaps/overlaps.
`StartOfDay` uses the same policy for historical midnight transitions. Use
`DateTime.Add(duration)` for elapsed arithmetic. Helpers return immutable values.

## Framework defaults and explicit overrides

| Feature | Default and override |
| --- | --- |
| Date helpers | `s.Time()` uses `Settings.TimeZone`; `In(zone)` overrides |
| Calendar schedules | `s.Calendar()` uses `Settings.TimeZone`; `In(zone)` overrides |
| JSON logging and daily file rollover | Each sink inherits the application zone; `Sink.TimeZone` overrides |
| Datatable export presentation | Report `Config.TimeZone` inherits the application zone; per-export `Presentation.TimeZone` overrides |

Inside an application schedule constructor:

```go
calendar := s.Calendar()
daily, err := calendar.DailyAt("reports.daily", "09:00", handler)
```

Enable/configure the scheduler and coordination as described in the
[scheduler guide](scheduler.md). Calendar also supplies `Cron`, `Hourly`, `Daily`,
`Weekly` and `Monthly`; existing package functions accept explicit locations.
Cron skips DST gaps and emits each distinct occurrence in an overlap. Interval
schedules, TTLs, timeouts and retention ages measure elapsed time.

Log timestamps include the selected offset and daily files roll on the first
write after the local date changes. Archive filenames retain UTC timestamps.
Borrowed/custom loggers keep their own behavior. See [logging](logging.md).
Direct `logging.JSON` defaults to UTC unless `Options.Location` is supplied;
direct prepared sinks use `SinkConfig.TimeZone`, with empty selecting UTC.
Export formatting receives the chosen presentation zone; scalar/JSON codecs keep
their existing wire contracts. See [datatables](datatable.md).

Database instants and `temporal.DateTime` JSON stay UTC. SQL calendar operations
retain their explicit timezone arguments. Raw `time.Now()`, custom formatters and
third-party libraries do not automatically read application settings; pass
`dates.Location()` or retain `dates` when they need the application zone.
The framework never changes `time.Local` or process environment variables.

The [independent consumer](../../tests/fixtures/consumer/configuredprofile/timezone_test.go)
exercises configuration, constructor injection, application isolation, schedules
and real file logging.
