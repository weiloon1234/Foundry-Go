# HTTP kernel

The HTTP kernel integrates standard `net/http` handlers with Foundry's
[application lifecycle](foundation.md). This guide covers server ownership and
request deadlines. [Typed endpoints](http-endpoints.md)
compose generated input and response contracts on this same kernel. The
[HTTP blueprint](../../blueprint/08-http-validation-and-responses.md) owns that
milestone scope.

## Application registration

Declare a typed server key and register `http.Module` with a handler constructor.
The constructor receives the existing `foundation.Resolver`; resolve concrete
domain services once here and pass them to handlers. It runs during `Build`,
without binding a port. Only `app.Run(ctx, foundation.HTTP)` binds the listener,
after all providers have booted. Selecting a worker or another kernel leaves the
HTTP listener unopened.

```go
var serverKey = foundation.NewKey[*foundryhttp.Server]("app.http")

config := foundryhttp.DefaultServerConfig()
config.Address = "127.0.0.1:8080"

module := foundryhttp.Module("http", serverKey, config,
    func(resolver foundation.Resolver) (http.Handler, error) {
        // Construct your standard handler with resolved domain dependencies.
        return http.NotFoundHandler(), nil
    },
)
app, err := foundry.New().Register(module).Build(ctx)
if err != nil {
    return err
}
return app.Run(ctx, foundation.HTTP)
```

Here `foundryhttp` imports `github.com/weiloon1234/Foundry-Go/http`, `http` imports
`net/http`, and `foundry` imports the framework root. This is native handler
interoperability; it does not infer request/response schemas or expose model
fields as JSON. The independent [consumer fixture](../../tests/fixtures/consumer/httpkernel/kernel_test.go)
compiles public registration and tests injected service ownership through real TCP
requests.

`Prepare(handler, config, logger)` supports standalone server ownership. It needs
an explicit `*slog.Logger` and opens no resources. Start it with `server.Run(ctx)`.
A module-created server must run through its application. Both forms permit only
one startup attempt; prepare a new server for a deliberate retry.

## Configuration and readiness

`DefaultServerConfig` returns explicit defaults that may be modified before
construction. Timeouts and `MaxHeaderBytes` must be positive. Read/write/header
and idle timeouts bound native connection I/O. `RequestTimeout` separately bounds
the request context after admission. It defaults to 25 seconds, leaving time
within the default 30-second native write timeout for a cooperative response.
Earlier parent deadlines remain in effect. Completion cancels the derived
context and releases its timer without canceling its parent.

Cooperative handlers, database operations and outbound calls receive the same
context. Its expiry does not kill Go computations or release application
resources while code is still using them. Native I/O deadlines and connection
shutdown own blocked network operations. Raw handlers own their timeout response;
typed endpoint adapters map observed request cancellation to the shared timeout
error before committing a response. A timeout does not undo committed domain work.
[Request-body limits, attribution and typed error responses](http-requests.md)
apply independently of the context deadline.

For an operating-system-assigned port use `127.0.0.1:0`. Resolve the module's
server key and call `server.Ready(ctx)` to await the actual bound address or the
startup error. The caller can cancel this wait. Readiness records a successful
bind; it is not a continuing health check or a trusted public origin. The current
server binds plain TCP HTTP; TLS and proxy configuration require their explicit
transport integration.

## Shutdown and raw handler behavior

Kernel cancellation stops new handler admission and begins the configured
shutdown grace. Active request contexts retain their values and remain usable
during that grace, subject to their existing request deadlines. If the shutdown
grace expires, Foundry cancels them and closes ordinary server
connections. `Run` still waits for admitted handlers to actually exit, keeping
their application dependencies alive. `Done` closes only at that point. An
application caller's shorter shutdown deadline bounds its wait, not ownership.

Raw handlers receive the original standard response writer, preserving streaming
and `ResponseController` capabilities. A panic aborts the response and produces a
safe diagnostic without the panic payload; `http.ErrAbortHandler` remains silent.
Foundry does not append an error document to a response a raw handler may already
have started. Typed endpoints prepare responses before committing headers. The
[built-in typed error response](http-requests.md) is available to raw handlers.

A hijacked connection is no longer owned by the standard server. Foundry retains
the synchronous handler's lifetime and applies the same grace to its context.
Raw code must own and close a hijacked connection, and work it launches after
returning needs a separate application lifecycle owner. Framework-managed
WebSockets and their distributed shutdown are owned by the realtime hub. These
boundaries follow the standard server's [shutdown contract](https://pkg.go.dev/net/http#Server.Shutdown).

Focused race tests cover normal requests, graceful completion, forced connection
closure with unfinished handlers, hijacked handler cancellation, binding failure,
single-use startup, raw panic/abort/Goexit, and application dependency retention.
The kernel passed full repository acceptance. The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md) records the complete milestone status.

Milestone 24 delivered verified production admission: `MaxConnections` defaults
to 4,096 accepted connections and reserves capacity before native Accept, including
slow header readers. Additional peers wait in the OS listen backlog. Hijacks keep
their connection permit until their explicit owner closes them. The listener's
closure stops admission without claiming those transferred connections have ended.
`MaxConcurrentRequests` defaults to 1,024 live handlers; exhaustion returns 503.
Cancelled handlers retain their slot until actual return. Zero on either new field
selects its default; both have a maximum of 1,048,576. These are configurable
resource ceilings, not a claim that every maximum fits every host.

Server configuration snapshots its maintenance-path slice. Read-only operational
exceptions, trace trust and shared metrics are described in
[production diagnostics](production-diagnostics.md) and
[observability](observability.md). Native optional writer capabilities remain
available through the same response-control adapter used by compression and ETags.
