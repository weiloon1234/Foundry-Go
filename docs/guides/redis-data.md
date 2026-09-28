# Typed Redis hashes and sets

`redis/data` supplies ordinary typed handles over an explicitly configured Redis
client. Use hashes for fields belonging to a resource and sets for membership.
These structures have their own addresses and are unaffected by cache invalidation.
The [independent consumer](../../tests/fixtures/consumer/redisdata/members.go) is the
compiling source for the examples below. No extra dependency is required.

## Declare once, bind explicitly

```go
type ProfileField string
const DisplayProfile ProfileField = "display"
type Profile struct {
    Email mutatorqueries.DisplayEmail
    Labels []string
}
var ProfileHashes = data.DefineHash[model.ID[mutatorqueries.Member], ProfileField, Profile](
    "member-profiles", 1,
    keyspace.TextKeys[model.ID[mutatorqueries.Member]](),
    keyspace.StringKeys[ProfileField](),
)
var Groups = data.DefineSet[model.ID[mutatorqueries.Member], model.ID[models.Group]](
    "member-groups", 1, keyspace.TextKeys[model.ID[mutatorqueries.Member]](),
)

store, err := data.NewStore(client, data.DefaultConfig(namespace))
// Handle err, then bind the declarations.
profiles, err := ProfileHashes.Bind(store)
groups, err := Groups.Bind(store)
```

The store borrows the client; its owner starts and closes it through the existing
[Redis lifecycle](redis.md). Construction and binding perform no I/O. Names and
versions identify contracts, and separate declarations cannot reuse one name/version
inside a store. Rebinding the same declaration is allowed. Processes sharing a
namespace must share declarations, codecs and bounds. Increment the version for an
incompatible field or payload schema. Different namespaces, kinds and versions use
different physical addresses; no ambient namespace discovery takes place.

## Read and write typed fields

```go
email, err := member.AccessEmail()
// Handle the getter error before creating the DTO.
added, err := profiles.Set(ctx, member.ID, DisplayProfile,
    Profile{Email: email, Labels: []string{"active"}})
profile, found, err := profiles.Get(ctx, member.ID, DisplayProfile)
removed, err := profiles.DeleteField(ctx, member.ID, DisplayProfile)
count, err := profiles.Count(ctx, member.ID)
```

`Set` reports whether it added a field; replacing an existing field returns false
with no error. `Get` distinguishes a missing field from a present JSON null. Declare
nullable values with `value.Nullable[T]` where appropriate. Typed values use Foundry's
canonical JSON snapshot and schema validation; reads return independent maps and
slices. Unknown fields, incompatible shapes and noncanonical stored encodings fail
without returning a partial result. Hash field identity comes from its explicit
`keyspace.Codec[F]`; field bytes are bounded and the codec must be injective.

Select getters when assembling presentation DTOs. Foundry preserves stored model
fields and never automatically exposes a model as a JSON response. The consumer
fixture verifies that a getter-selected email survives the hash round trip.

## Add, remove and enumerate members

```go
added, err := groups.Add(ctx, member.ID, group.ID)
present, err := groups.Contains(ctx, member.ID, group.ID)
members, err := groups.Members(ctx, member.ID)
removed, err := groups.Remove(ctx, member.ID, group.ID)
count, err := groups.Count(ctx, member.ID)
```

Membership equality is the canonical JSON representation, including normalized
object key order and numeric spelling. Custom JSON/text methods must be deterministic,
concurrency-safe, preserve inputs and decode owned values. The compiler rejects
unrelated model IDs as resource keys or members. `Members` returns an owned native
slice, including an empty slice for a missing set, sorted by encoded JSON bytes.
This order is deterministic; apply native slice sorting for domain or numeric order.

## Existence, expiry and deletion

Both handles expose `Exists`, `Expire`, `Count`, `Delete` and `DeleteMany` while
retaining the declaration's resource key type:

```go
exists, err := groups.Exists(ctx, member.ID)
changed, err := groups.Expire(ctx, member.ID, cache.For(time.Hour))
changed, err = groups.Expire(ctx, member.ID, cache.Forever())
deleted, err := groups.DeleteMany(ctx, firstMember.ID, secondMember.ID)
```

New hashes and sets are persistent. Field/member writes retain any existing key
expiry. Removing the last field or member removes the key. `Expire` preserves
contents, returns false for a missing key and true for an existing key, even if it
was already persistent. Positive durations round up to whole milliseconds. The
expiry type reuses `cache.TTL`; its zero value is invalid.

A write followed by `Expire` is two separate operations. It does not provide atomic
initialization with expiry. This explicit behavior follows native Redis writes and
[expiry retention](https://redis.io/docs/latest/commands/expire/); higher-level
session protocols must own any required transactional initialization.

`DeleteMany` checks the input bound before deduplicating and encoding all keys. It
validates the complete batch before one exact atomic deletion. Counts exclude missing
and repeated keys. It does not enumerate keys, clear another namespace, or decode
payloads. Stored type corruption rejects the whole batch; deletion can still remove
an oversized collection of the declared type.

## Bounds, ownership and failures

Default limits are 1024 members/fields, 1024 field bytes, 4096 encoded value bytes
and 4 MiB of member payload per reply. Cardinality is capped at 4096, reply payload
at 16 MiB, and `Entries * ValueBytes` must fit `ReplyBytes`. Hash values use the same
per-value limit, but point reads do not enumerate the hash. The Redis client's
`MaxValueBytes` can impose a smaller ceiling. Bound changes across clients can cause
reads or writes to fail; deploy a consistent declaration configuration.

A full collection allows replacement of an existing hash field or re-addition of
an existing member, while rejecting growth. Native Redis checks type, cardinality
and relevant payload sizes in the same script as the operation. Read validation
precedes the sole mutation command. A set reply checks cardinality before enumeration
and member lengths before returning payloads. Redis must materialize externally
oversized members to inspect their sizes; this is not a hard server-memory bound
against arbitrary external writes or a malicious RESP server. Adapters and raw callers
must preserve the typed schema and canonical value representation.

Store defaults separately bound declarations, key bytes, 128 active operations,
64 input batch keys and a five-second operation deadline. Batch size has a shared
hard cap of 256 keys. Key/JSON/adapter callbacks remain owned until they actually
exit, including after cancellation. Panics and Goexit become errors; a callback that
ignores cancellation retains its slot instead of being abandoned. Keep input maps,
slices and callback-backed keys unchanged until the call returns.

Remote failures return zero results and errors. An acknowledgement can be lost after
a write succeeds; Foundry never retries that mutation automatically or falls back to
another authority. Script atomicity does not imply rollback after an arbitrary
server runtime error, or a transaction with PostgreSQL. This slice uses one mutation
command per operation after its validation reads. Advanced operations use the explicit [scoped command boundary](redis-commands.md),
with `AdapterKey` reusing the typed declaration’s actual key resolution.

Focused native behavior, races, ten compiler rejection cases and five real-gopls
scenarios passed. Full native verification passed in 444.1 seconds.
