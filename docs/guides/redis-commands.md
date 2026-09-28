# Scoped Redis commands, pipelines and scripts

`redis/raw` is Foundry's explicit boundary for Redis capabilities outside the normal
[typed cache](caching.md), [hash/set](redis-data.md), [lease](leases.md),
[rate-limit](rate-limiting.md) and [pub/sub](pubsub.md) APIs. The
[independent consumer](../../tests/fixtures/consumer/rediscommands/commands.go)
contains compiling examples of scoped model keys, typed command/script results and
heterogeneous pipeline results. No vendor client escapes this boundary.

## Declare and resolve keys

```go
var Visits = raw.DefineKeys[model.ID[Member]](
    "member-visits", 1, keyspace.TextKeys[model.ID[Member]](),
)
store, err := raw.NewStore(client, raw.DefaultConfig(namespace))
// Handle err before binding.
keys, err := Visits.Bind(store)
key, err := keys.For(ctx, member.ID)
```

The store borrows the existing [Redis client](redis.md). Construction and binding
perform no I/O. A declaration preserves the domain key type; unrelated model IDs
fail compilation. Declare a name/version once and reuse it. Another declaration
cannot occupy that pair in one store. `keys.Name`, `keys.Version` and
`store.Namespace` expose the configuration used for resolution.

Addresses combine application/environment, feature, name, version and a bounded
hash of the logical key. Raw addresses are separate from cache and coordination
metadata. `Key.String` is an explicit adapter address; logical suffixes are not
retained. To select another authorized namespace, construct a separate configured
store. One invocation or pipeline rejects mixed namespaces.

For an advanced operation on an existing typed hash/set, use
`profiles.AdapterKey(ctx, member.ID)` followed by `raw.FromDataKey`. This reuses its
actual declaration and key codec. Preserve the data contract when extending it;
raw writes must not introduce invalid JSON or violate collection bounds.

## Build immutable commands with concrete results

```go
command := raw.NewCommand("ZREVRANGE", raw.DecodeArray(raw.DecodeString())).
    Key(scoreboardKey).
    Arg(raw.Int64(0)).Arg(raw.Int64(9)).Arg(raw.Text("WITHSCORES"))
ranking, err := command.Run(ctx, store) // []string
```

`NewCommand` initially returns a builder with no `Run` method. `Key` creates the
executable `Command[R]`. Add further keys with `Key`, and raw values with `Arg`.
`Text`, `Bytes`, `Int64` and `Uint64` construct explicit arguments; byte arguments
are snapshotted. Commands and builders are immutable values: appending to a copy
does not change the original. Invalid constructors or oversize arguments fail
before execution. Prefix arguments support commands such as
`NewCommand("XGROUP", DecodeString()).Arg(Text("CREATE")).Key(streamKey)`.

Decoders include exact signed/unsigned integers, booleans restricted to 0/1,
strings, owned bytes, arrays and `value.Nullable[T]`. Redis nil is distinct from a
server/transport error and requires a nullable decoder when it is allowed.
`DecodeWith` accepts `func(context.Context, raw.Reply) (R, error)` for custom typed
results. `Reply` is immutable and exposes checked integer/text/array accessors.
Array access returns a fresh slice of immutable child replies. Normal application
returns remain concrete Go types; dynamic RESP values belong inside adapters and
explicit custom decoders.

## Queue heterogeneous typed results

```go
pipeline, err := raw.NewPipeline(raw.Transaction)
// Handle err before queueing.
err = raw.Ignore(pipeline,
    raw.NewCommand("SET", raw.DecodeString()).Key(key).Arg(raw.Int64(0)))
count, err := raw.Queue(pipeline,
    raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(key).Arg(raw.Int64(1)))
stored, err := raw.Queue(pipeline,
    raw.NewCommand("GET", raw.DecodeString()).Key(key))
// Handle every queue error before Run.
err = pipeline.Run(ctx, store)
// Handle err before reading results.
number, err := count.Value()  // int64
text, err := stored.Value()   // string
```

`Queue` is a generic package function because Go methods cannot declare their own
type parameters. Each receipt preserves its command's result type. No receipt
becomes successful until execution and every decoder have succeeded. `Ignore`
still checks server/protocol errors and its declared decoder; it discards only the
successful value. Results remain errors after a failed batch even when Redis
applied some commands. Repeated `Value` calls share the application's decoded value;
callers coordinate mutation of maps/slices they obtain from it.

A pipeline is built and executed once, including when validation or cancellation
fails. Queueing and starting are synchronized; additions after `Run` claims the
queue fail. `Len` and `IsEmpty` describe commands still queued; `Mode` retains the
selected execution mode. Build another pipeline for a deliberate new attempt.

| Mode | Execution and failure behavior |
| --- | --- |
| `raw.Pipelined` | Sends commands as one batch. Other clients may interleave work; failures can follow partial execution. |
| `raw.Transaction` | Uses MULTI/EXEC so other clients do not interleave during execution. Runtime errors do not roll back successful sibling commands. |

These are Redis's [pipelining](https://redis.io/docs/latest/develop/using-commands/pipelining/)
and [transaction](https://redis.io/docs/latest/develop/using-commands/transactions/)
semantics. Neither mode is a transaction with PostgreSQL. A missing first reply does
not hide a later command error.

## Trusted scripts and explicit key operations

```go
script := raw.NewScript(
    "return redis.call('INCRBY',KEYS[1],ARGV[1])",
    key, raw.DecodeInt64(),
).Arg(raw.Int64(3))
count, err := script.Run(ctx, store)
```

Scripts require a key and use one EVAL, without an EVALSHA/NOSCRIPT fallback.
Additional `Key` and `Arg` calls populate KEYS and ARGV in their respective order.
A script returns `Command[R]`, so it can also be queued in a pipeline. Lua source,
command argument positions and their Redis semantics remain trusted application
code. Always use declared KEYS for key access; Foundry cannot prove arbitrary
source or text arguments obey that rule. See Redis's
[scripting semantics](https://redis.io/docs/latest/develop/programmability/eval-intro/).
Lua numeric behavior still applies to script results; use the normal typed counter
API when its exact arithmetic guarantees are needed.

Bound `Keys[K]` also expose `Exists`, `Expire`, `Delete` and bounded `DeleteMany`
for native strings, hashes, sets, sorted sets and streams. `Expire` reuses
`cache.TTL`, preserves contents, returns false for a missing key and true for an
existing one, including repeated `Forever`. Batch deletion encodes every key before
one DEL and counts distinct existing keys. None of these methods enumerate a
namespace or retry an uncertain mutation.

Commands that change pooled connection state are rejected: authentication/database
selection, connection control, subscriptions, WATCH and manual transaction control
belong to dedicated lifecycle or feature APIs. Direct EVAL/EVALSHA commands are also
rejected in favor of `NewScript`. Use typed leases or a key-bound script for supported
atomic coordination. This API does not expose a long-lived watched connection.

## Bounds, ownership and uncertain outcomes

Defaults bound each invocation or entire pipeline to 1 MiB of argument/source/name
payload and 64 commands, with at most 256 arguments per command. Construction has
hard caps of 256 commands, 16 MiB of combined request payload, 1 MiB per argument
and 64 KiB of script source. Empty text/byte arguments remain valid. Payload counts
exclude RESP framing; bounded argument/command counts also bound framing overhead.

Default reply limits are 1 MiB of string payload, 8192 tree nodes and depth 32.
A batch shares one budget including its synthetic array root. Hard limits are
16 MiB, 65536 nodes and depth 64. Driver parsing precedes capture, so these do not
claim to bound allocations by a malicious RESP server. Trusted commands/scripts
must impose appropriate server-side work and response limits; client deadlines do
not roll back server work or forcibly stop Lua execution.

Store defaults bound 128 active operations, 256 declarations, 1024 logical key bytes
and five seconds per operation. Context cancellation can shorten the deadline.
Key and decoder callbacks receive owned execution: panics/Goexit become errors,
and canceled callbacks retain their slot until they actually exit. Custom decoders
must honor context, preserve inputs and return owned results. Command failures and
server errors use redacted ordinary error messages.

The client and adapter never retry an uncertain command, script or pipeline. Losing
an acknowledgement can leave a completed write unconfirmed; returned typed values
remain zero/error. Redis persistence, access permissions and failover behavior are
server deployment concerns. This client continues to target explicit standalone
Redis connections.

Focused native behavior/races, twelve compiler rejection cases and six real-gopls
scenarios passed. Full native verification passed in 463.5 seconds; milestone 09
passed its source/parity closure review.
