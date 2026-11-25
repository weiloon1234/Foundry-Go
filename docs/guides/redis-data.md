# Typed Redis hashes, sets, sorted sets and lists

`redis/data` supplies ordinary typed handles over an explicitly configured Redis
client. Use hashes for fields belonging to a resource, sets for membership,
sorted sets for scored rankings and lists for bounded ordered sequences.
These structures have their own addresses and are unaffected by cache invalidation.
The [independent consumer](../../tests/fixtures/consumer/redisdata/members.go) is the
compiling source for the examples below. No extra dependency is required.
The fixture supplies `groupID` as a separate `model.ID[models.Group]` Redis key.
It is not the database primary key of `models.Group`, which uses `Code`.

## Declare once, bind explicitly

In these recipes, `Member` is the application model with its typed `ID` and
`AccessEmail` accessor. `DisplayEmail` is the accessor's application-owned result type.

<!-- compiled-source: tests/fixtures/consumer/redisdata/members.go#redis-data-1 -->
```go
type ProfileField string

const DisplayProfile ProfileField = "display"

type Profile struct {
	Email  DisplayEmail
	Labels []string
}
type Profiles = data.Hash[model.ID[Member], ProfileField, Profile]
type MemberGroups = data.Set[model.ID[Member], model.ID[models.Group]]

var ProfileHashes = data.DefineHash[model.ID[Member], ProfileField, Profile]("member-profiles", 1, keyspace.TextKeys[model.ID[Member]](), keyspace.StringKeys[ProfileField]())
var Groups = data.DefineSet[model.ID[Member], model.ID[models.Group]]("member-groups", 1, keyspace.TextKeys[model.ID[Member]]())
```

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-1 -->
```go
func bindStructures(client *redis.Client, namespace keyspace.Namespace) error {
	store, err := data.NewStore(client, data.DefaultConfig(namespace))
	if err != nil {
		return err
	}
	profiles, err := ProfileHashes.Bind(store)
	if err != nil {
		return err
	}
	groups, err := Groups.Bind(store)
	if err != nil {
		return err
	}
	_ = profiles
	_ = groups
	return err
}
```

The store borrows the client; its owner starts and closes it through the existing
[Redis lifecycle](redis.md). Construction and binding perform no I/O. Names and
versions identify contracts, and separate declarations cannot reuse one name/version
inside a store. Rebinding the same declaration is allowed. Processes sharing a
namespace must share declarations, codecs and bounds. Increment the version for an
incompatible field or payload schema. Different namespaces, kinds and versions use
different physical addresses; no ambient namespace discovery takes place.

`Hash`, `Set`, `SortedSet` and `List` embed the exported defined type
`data.Handle[K]`. Its documented methods are `Handle.Name`, `Handle.Version`,
`Handle.Exists`, `Handle.Expire`, `Handle.Count`, `Handle.Delete`,
`Handle.DeleteMany` and `Handle.AdapterKey`. Existing calls such as
`profiles.Count(ctx, key)` remain valid. Obtain a bound container through its
declaration's `Bind` method. A zero `Handle` is unbound: `Name` and `Version`
return zero values, and operations requiring a store return an error.

## Read and write typed fields

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-2 -->
```go
func profileFields(ctx context.Context, member Member, profiles Profiles) error {
	email, err := member.AccessEmail()
	if err != nil {
		return err
	}
	added, err := profiles.Set(ctx, member.ID, DisplayProfile,
		Profile{Email: email, Labels: []string{"active"}})
	if err != nil {
		return err
	}
	profile, found, err := profiles.Get(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	removed, err := profiles.DeleteField(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	count, err := profiles.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	_ = added
	_ = profile
	_ = found
	_ = removed
	_ = count
	return err
}
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

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-3 -->
```go
func groupMembers(ctx context.Context, member Member, groupID model.ID[models.Group], groups MemberGroups) error {
	added, err := groups.Add(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	present, err := groups.Contains(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	members, err := groups.Members(ctx, member.ID)
	if err != nil {
		return err
	}
	removed, err := groups.Remove(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	count, err := groups.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	_ = added
	_ = present
	_ = members
	_ = removed
	_ = count
	return err
}
```

Membership equality is the canonical JSON representation, including normalized
object key order and numeric spelling. Custom JSON/text methods must be deterministic,
concurrency-safe, preserve inputs and decode owned values. The compiler rejects
unrelated model IDs as resource keys or members. `Members` returns an owned native
slice, including an empty slice for a missing set, sorted by encoded JSON bytes.
This order is deterministic; apply native slice sorting for domain or numeric order.

## Batch reads and exact counters

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-4 -->
```go
func manyProfiles(ctx context.Context, member Member, profiles Profiles) error {
	values, err := profiles.GetMany(ctx, member.ID, DisplayProfile /*, more fields */)
	if err != nil {
		return err
	}
	display, found := values[0].Get() // value.Optional[Profile], in input order
	_ = display
	_ = found
	return err
}
```

`GetMany` reads up to `Limits.Entries` fields in one atomic script. The result has
one `value.Optional[V]` per requested field in input order; a missing field is unset
and a repeated field repeats its value. Stored sizes are checked against the value
and reply budgets before any payload is transferred.

`GetAll` returns every field as `[]data.HashEntry[F, V]`, ordered by encoded field
bytes (the adapter sorts them, independent of the Redis server's locale). Because key codecs only encode, the declaration must supply the inverse. The
[rankings consumer](../../tests/fixtures/consumer/redisdata/rankings.go) declares:

<!-- compiled-source: tests/fixtures/consumer/redisdata/rankings.go#redis-activity-types -->
```go
type ActivityKind string

const PageViews ActivityKind = "page-views"

// Activity is a consumer-owned DTO stored in a bounded recent-activity list.
type Activity struct {
	Member model.ID[Member]
	Kind   ActivityKind
}
type ActivityCounts = data.Hash[model.ID[Member], ActivityKind, int64]
```

<!-- compiled-source: tests/fixtures/consumer/redisdata/rankings.go#redis-data-5 -->
```go
var Counts = data.DefineHash[model.ID[Member], ActivityKind, int64]("member-activity", 1, keyspace.TextKeys[model.ID[Member]](), keyspace.StringKeys[ActivityKind]()).
	WithFieldDecoder(func(text string) (ActivityKind, error) { return ActivityKind(text), nil })
```

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-5 -->
```go
func activityCounts(ctx context.Context, member Member, counts ActivityCounts) error {
	entries, err := counts.GetAll(ctx, member.ID)
	if err != nil {
		return err
	}
	total, err := data.IncrementField(ctx, counts, member.ID, PageViews, 1)
	if err != nil {
		return err
	}
	_ = entries
	_ = total
	return err
}
```

`WithFieldDecoder` keeps the declaration's name/version identity. Every decoded field
must encode back to its exact stored bytes, otherwise the read fails. Without a
decoder, `GetAll` returns `fault.Invalid`.

`IncrementField` is a package function because only `Hash[K, F, int64]` supports it;
the compiler rejects it for other value types. It adds a signed delta with Redis
64-bit arithmetic and returns the result. A missing field starts at zero, existing
expiry is retained, and a full hash rejects a new field. A stored value that is not a
canonical integer, or an overflow, fails with `fault.Invalid` without mutation.
The value bound must allow 20 bytes.

## Scored rankings with sorted sets

<!-- compiled-source: tests/fixtures/consumer/redisdata/rankings.go#redis-data-6 -->
```go
var Rankings = data.DefineSortedSet[model.ID[models.Group], model.ID[Member]]("group-rankings", 1, keyspace.TextKeys[model.ID[models.Group]]())
```

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-6 -->
```go
func rankedMembers(ctx context.Context, member Member, store *data.Store, groupID model.ID[models.Group]) error {
	rankings, err := Rankings.Bind(store)
	if err != nil {
		return err
	}
	added, err := rankings.Add(ctx, groupID, member.ID, 1250)
	if err != nil {
		return err
	}
	score, err := rankings.Increment(ctx, groupID, member.ID, 25)
	if err != nil {
		return err
	}
	top, err := rankings.Range(ctx, groupID, data.Last(10)) // []data.Scored[model.ID[Member]]
	if err != nil {
		return err
	}
	rank, found, err := rankings.Rank(ctx, groupID, member.ID, data.Descending)
	if err != nil {
		return err
	}
	window, err := rankings.RangeByScore(ctx, groupID, data.Between(1000, 2000), data.First(20))
	if err != nil {
		return err
	}
	count, err := rankings.CountByScore(ctx, groupID, data.ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)})
	if err != nil {
		return err
	}
	_ = added
	_ = score
	_ = top
	_ = rank
	_ = found
	_ = window
	_ = count
	return err
}
```

Members use canonical JSON identity, as in sets; each carries a `float64` score.
Order follows Redis: by score, then by encoded member bytes (`Descending` reverses
both). `data.Window` selects `data.Window.Count` members after `Offset` (`First(n)`/`Last(n)`
are shortcuts); `data.Window.Count` must be within `Limits.Entries`. `data.ScoreRange` bounds
are inclusive unless `ExcludeMin`/`ExcludeMax` is set; use `math.Inf` for open
ends. NaN scores, NaN increment results and invalid ranges fail with
`fault.Invalid`. `Add` reports whether the member is new; a full sorted set can
re-score existing members but rejects growth. `Score` and `Rank` distinguish a
missing member. Replies are checked for window size, order and payload bounds.

## Bounded lists

<!-- compiled-source: tests/fixtures/consumer/redisdata/rankings.go#redis-data-7 -->
```go
var Recent = data.DefineList[model.ID[models.Group], Activity]("group-activity", 1, keyspace.TextKeys[model.ID[models.Group]]())
```

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-7 -->
```go
func recentActivity(ctx context.Context, member Member, store *data.Store, groupID model.ID[models.Group], newest Activity) error {
	recent, err := Recent.Bind(store)
	if err != nil {
		return err
	}
	err = recent.Trim(ctx, groupID, -99, -1) // keep the newest 99 before pushing
	if err != nil {
		return err
	}
	length, err := recent.Push(ctx, groupID, Activity{Member: member.ID, Kind: PageViews})
	if err != nil {
		return err
	}
	length, err = recent.PushFront(ctx, groupID, newest)
	if err != nil {
		return err
	}
	items, err := recent.Range(ctx, groupID, 0, -1)
	if err != nil {
		return err
	}
	item, found, err := recent.PopFront(ctx, groupID)
	if err != nil {
		return err
	}
	_ = length
	_ = items
	_ = item
	_ = found
	return err
}
```

A list's length is bounded by `Limits.Entries`: a push that would exceed it inserts
nothing and fails with `fault.Invalid`, so trim before pushing when you want a
capped window. `Push` appends in order; `PushFront` inserts each value at the head
in turn (like Redis LPUSH), so the last argument becomes the first element. Both
return the new length. `Range` and `Trim` use Redis index semantics, where negative
indices count from the tail. A pop checks the element's size before removing it;
an element that then fails JSON decoding is reported as an error and stays removed.

## Existence, expiry and deletion

Every handle exposes `Exists`, `Expire`, `.Count`, `Delete` and `DeleteMany` while
retaining the declaration's resource key type:

<!-- compiled-source: tests/fixtures/consumer/redisdata/guide_operations.go#redis-data-8 -->
```go
func groupLifetime(ctx context.Context, member Member, groups MemberGroups, firstMember, secondMember Member) error {
	exists, err := groups.Exists(ctx, member.ID)
	if err != nil {
		return err
	}
	changed, err := groups.Expire(ctx, member.ID, cache.For(time.Hour))
	if err != nil {
		return err
	}
	changed, err = groups.Expire(ctx, member.ID, cache.Forever())
	if err != nil {
		return err
	}
	deleted, err := groups.DeleteMany(ctx, firstMember.ID, secondMember.ID)
	if err != nil {
		return err
	}
	_ = exists
	_ = changed
	_ = deleted
	return err
}
```

New hashes, sets, sorted sets and lists are persistent. Field, member and element
writes retain any existing key expiry. Removing the last field, member or element
removes the key. `Expire` preserves
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
64 input batch keys and a five-second operation deadline. When all operation slots
are busy, a call queues in FIFO order for at most the operation timeout (capped at
five seconds) and its own deadline, then returns retryable `fault.Overloaded`,
which HTTP maps to 503 with `Retry-After`. Batch size has a shared
hard cap of 256 keys. Key/JSON/adapter callbacks remain owned until they actually
exit, including after cancellation. Panics and Goexit become errors; a callback that
ignores cancellation retains its slot instead of being abandoned. Keep input maps,
slices and callback-backed keys unchanged until the call returns.

Remote failures return zero results and errors. An acknowledgement can be lost after
a write succeeds; Foundry never retries that mutation automatically or falls back to
another authority. Script atomicity does not imply rollback after an arbitrary
server runtime error, or a transaction with PostgreSQL. Each operation uses one
mutation command after its validation reads. Advanced operations use the explicit [scoped command boundary](redis-commands.md),
with `AdapterKey` reusing the typed declaration’s actual key resolution.

Focused native behavior, races, ten compiler rejection cases and five real-gopls
scenarios passed. Full native verification passed in 444.1 seconds.
