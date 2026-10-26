# Migrate generated models to the ORM

Replace generated query, draft and projection helpers with handwritten declarations in
`database/orm`, `database/orm/rows` and `database/orm/expr`. Keep your model structs,
stored column names, key types, codecs, constraints and public response contracts.
This guide describes APIs present in the Go-way branch, not every illustrative API
in the original plan.

Migrate a shared model package before its callers. Register its tables, port its
queries and tests, then remove the old model directive and generated helpers at a
coordinated generation barrier. Keep DTO, enum, configuration and client generation.
Do not edit generated files or ownership manifests by hand. Model and projection
generation is retired; legacy query runtime retirement is a separate step.

## Use the compiled examples

The after blocks are copied verbatim from the
[compiled example package](../../tests/fixtures/goway/migration/examples_test.go) or its
[SQL examples](../../tests/fixtures/goway/migration/sql_test.go) or the
[pilot boundary examples](../../tests/fixtures/goway/migration/pilot_test.go).
[The guide test](../../tests/fixtures/goway/migration/guide_test.go) checks those blocks
against their source. Before blocks are historical documentation and are no longer
compiled. Functions that accept an executor compile without opening connections.
Declaration validation and frozen SQL/binding comparisons run locally.
Run `go test ./migration` from `tests/fixtures/goway`.

Historical snippets use `old`, `oldhooks`, `oldreads` and `oldmutators` for retired
generated fixtures. Compiled examples use `models`, `hooks`, `reads`, `mutators` and
shared `coremodels` input/storage types. `reports` contains native row declarations;
`Users` without a package qualifier is the standalone declaration below. Copy the
current imports and concrete model types from the linked source.

## Declare models and lifecycle roles

Old declarations use `//foundry:model table=...` and generated `XFields()`/`XDraft`.
Historical lifecycle declarations used `hooks=LocalHooks`. The ordinary struct and its domain methods survive migration.
The old field namespace is obtained like this:

<!-- orm-example: roles-before -->
```go
func legacyRoles() oldhooks.MemberFieldSet {
	return oldhooks.MemberFields()
}
```

Declare each persisted field once, using a pointer into the builder's model:

<!-- orm-example: roles-after -->
```go
type EmailInput string
type User struct {
	ID        int64
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
}

func (User) MutateEmail(input EmailInput) (string, error) {
	return strings.ToLower(strings.TrimSpace(string(input))), nil
}

type UserTable struct {
	orm.Table[User, int64]
	Primary   orm.Primary[User, int64]
	Email     orm.InputText[User, EmailInput, string]
	CreatedAt orm.CreatedAt[User, time.Time]
	UpdatedAt orm.UpdatedAt[User, time.Time]
	DeletedAt orm.SoftDelete[User, time.Time]
	Soft      orm.SoftDeletable[User]
}

var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable {
	table := orm.TableOf(b, &u.ID)
	deleted := orm.SoftDeleteOf(b, &u.DeletedAt)
	return UserTable{table, orm.PrimaryOf(b, table.Key()),
		orm.InputTextOf[User, EmailInput](b, &u.Email, nil),
		orm.CreatedAtOf(b, &u.CreatedAt), orm.UpdatedAtOf(b, &u.UpdatedAt),
		deleted, orm.SoftDeletes(deleted)}
})
```

Choose carriers by their role as well as their stored value type:

| Old declaration | Native declaration and consequence |
| --- | --- |
| `primary=ID` or the conventional ID | `TableOf(b, &m.ID)` declares the single primary key; `table.Key()` keeps its stored lookup type. |
| Primary field selected by insert-from-query | Add `PrimaryOf(b, table.Key())` only if the application maps that key in insert-select. A lookup `Key` is not an insert mapping target. |
| `MutateID(Input) (Key, error)` | `InputKeyOf` binds the input type to that same primary declaration. It is mutually exclusive with `PrimaryOf`; distinct input disables automatic UUID synthesis. |
| Managed `CreatedAt`/`UpdatedAt` convention | Use `CreatedAtOf`/`UpdatedAtOf`. `TimeOf` on a managed field fails startup with the required carrier name. `NoTimestamps()` disables conventions; explicit managed roles conflict with that option. |
| Nullable soft-delete timestamp | Bind `SoftDeleteOf` and expose `SoftDeletes(deleted)` as a `SoftDeletable[M]` table field such as `Users.Soft`. `NoSoftDeletes()` disables the convention. |
| A field with any mutator, even identical input/storage types | Use the matching `Input*` or `NullableInput*` carrier. Ordinary columns with a mutator fail startup. The setter takes input; comparisons and ordering take stored values. |
| Other persisted fields | Use `TextOf`, `NumberOf`, `IDOf`, `EnumOf`, `TimeOf`, `JSONOf`, `BytesOf`, password/encrypted constructors and nullable variants. Keep `foundry:"column=..."`, `default=database` and omission tags. |

For a model with only `CreatedAt` (`created_at`) or only `UpdatedAt`
(`updated_at`), keep the timestamp application-managed. The retired legacy generator's
`discoverTimestamps` left timestamp roles unassigned when either field was absent;
explicit `timestamps=true` instead rejected the missing pair. Declare `orm.NoTimestamps()`
and an ordinary `orm.TimeOf` column, and retain each call site's explicit `Set`.
Do not replace that column with `CreatedAtOf` or `UpdatedAtOf`, which require a pair.

After, for a lone creation field (use the same pattern for a lone update field):

<!-- orm-example: lone-timestamp-after -->
```go
type DeveloperEvent struct {
	ID        int64
	CreatedAt time.Time
}

type DeveloperEventTable struct {
	orm.Table[DeveloperEvent, int64]
	CreatedAt orm.Time[DeveloperEvent, time.Time]
}

var DeveloperEvents = orm.Define("developer_events", func(b orm.Builder[DeveloperEvent], e *DeveloperEvent) DeveloperEventTable {
	return DeveloperEventTable{orm.TableOf(b, &e.ID), orm.TimeOf(b, &e.CreatedAt)}
}, orm.NoTimestamps())

func insertDeveloperEvent(ctx context.Context, tx *database.Tx, createdAt time.Time) error {
	_, err := DeveloperEvents.Insert(ctx, tx, DeveloperEvents.CreatedAt.Set(createdAt))
	return err
}
```

`NumberKey` and `TextKey` preserve key roles. They do not make a primary key an
ordinary writable numeric/text field or grant numeric adjustment operations.
`MapInsert` rejects input fields and managed UpdatedAt; CreatedAt is eligible.
`MapUpdate` also rejects primary keys. Application wrappers cannot manufacture these
roles by embedding or converting a column.

Keep `Mutate<Field>` and `Access<Field>` methods as ordinary Go methods. Pass `nil`
as the input constructor's transform to discover a named mutator at startup, as above.
Use the constructor's typed transform for explicit bindings, including same-type
transformations. The legacy `orm.Mutate(fn)` option is rejected on ordinary columns;
Input constructors take their transform argument instead. Do not configure both
an explicit and a discovered mutator. Startup rejects orphan or invalid Mutate/Access
methods. Accessors are explicit calls, not automatic projection or JSON presentation.

Distinct input and stored types retain their own contracts. The following fixtures
use `EmailInput` for writes and `StoredEmail` for storage:

Before:

<!-- orm-example: inputs-before -->
```go
func legacyInput(input coremodels.EmailInput) oldmutators.MemberDraft {
	return oldmutators.MemberDraft{}.SetEmail(input)
}
```

After:

<!-- orm-example: inputs-after -->
```go
func input(input coremodels.EmailInput) orm.Assignment[mutators.Member] {
	return mutators.Members.Email.Set(input)
}
```

Before:

<!-- orm-example: input-key-before -->
```go
func legacyInputKey(id model.ID[oldmutators.KeyRecord]) oldmutators.KeyRecordDraft {
	return oldmutators.KeyRecordDraft{}.SetID(oldmutators.FreshID{Value: id})
}
```

After:

<!-- orm-example: input-key-after -->
```go
func inputKey(id model.ID[mutators.KeyRecord]) orm.Assignment[mutators.KeyRecord] {
	return mutators.KeyRecords.ID.Set(mutators.FreshID{Value: id})
}
```

The [input-key declaration](../../tests/fixtures/goway/models/mutators/key.go) uses
`InputKeyOf` over TableOf's single primary key. The stored ID still drives Find.
An accessor call is unchanged:

Before:

<!-- orm-example: accessors-before -->
```go
func legacyAccessor(member oldmutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}
```

After:

<!-- orm-example: accessors-after -->
```go
func accessor(member mutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}
```

## Replace read and write calls

### Read complete models

Before:

<!-- orm-example: reads-before -->
```go
func legacyRead(ctx context.Context, db database.Executor, id model.ID[old.Order]) error {
	_, err := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100)).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Find(ctx, db, id)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Where(old.OrderFields().ID.Eq(id)).RequireFirst(ctx, db)
	return err
}
```

After:

<!-- orm-example: reads-after -->
```go
func read(ctx context.Context, db database.Executor, id model.ID[models.Order]) error {
	_, err := models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = models.Orders.Find(ctx, db, id)
	if err != nil {
		return err
	}
	_, err = models.Orders.Query().Where(models.Orders.ID.Eq(id)).Require(ctx, db)
	return err
}
```

`Where`, `OrderBy`, `Limit`, `Offset`, `First`, `All`, `Count` and `Exists` stay on
typed model queries. Replace legacy `RequireFirst` with `Require` on model queries
and locked model queries; rows result adapters retain `RequireFirst`.
`ForUpdate` and other lock selections retain
the `*database.Tx` requirement. Scopes and soft-delete visibility stay attached.

### Insert and update assignments

Before:

<!-- orm-example: insert-before -->
```go
func legacyInsert(ctx context.Context, tx *database.Tx) error {
	_, err := old.QueryUsers().Create(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test"))
	return err
}
```

After:

<!-- orm-example: insert-after -->
```go
func insert(ctx context.Context, tx *database.Tx) error {
	_, err := models.Users.Insert(ctx, tx, models.Users.Email.Set("ada@example.test"))
	return err
}
```

Before:

<!-- orm-example: writes-before -->
```go
func legacyWrite(ctx context.Context, tx *database.Tx, id int64) error {
	draft := oldhooks.MemberDraft{}.SetName("Ada").ClearNote()
	_, err := oldhooks.QueryHookMembers().Update(ctx, tx, id, draft)
	return err
}
```

After:

<!-- orm-example: writes-after -->
```go
func write(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := hooks.Members.Update(ctx, tx, id,
		hooks.Members.Name.Set("Ada"), hooks.Members.Note.SetNull())
	return err
}
```

Omit an assignment to leave the field omitted. `Set(0)`, `Set(false)` and `Set("")`
are supplied values. `SetNull()` is explicit SQL NULL. Database defaults apply to
omitted database-default fields. Mutators skip omissions and SQL NULL; encryption
runs after validated effective assignments and identity/conventions preparation.

`InsertMany` takes `[][]orm.Assignment[M]`. Use `CreateEach`/`InsertEach` when each
input needs its model lifecycle. `UpdateEach`, `DeleteEach`, `RestoreEach` and
`ForceDeleteEach` preserve bounded, atomic per-model behavior and returned models.
A set write that would skip local hooks or observers needs explicit acknowledgement:

Before:

<!-- orm-example: set-writes-before -->
```go
func legacySetWrite(ctx context.Context, tx *database.Tx) error {
	_, err := oldhooks.QueryHookMembers().WithoutModelHooks().UpdateAll(ctx, tx, oldhooks.MemberDraft{}.SetName("guest"))
	return err
}
```

After:

<!-- orm-example: set-writes-after -->
```go
func setWrite(ctx context.Context, tx *database.Tx) error {
	_, err := orm.WithoutModelHooks(hooks.Members.Query()).UpdateAll(ctx, tx, hooks.Members.Name.Set("guest"))
	return err
}
```

Acknowledgement does not make set writes run per-model hooks or audit. Source writes
and upserts retain their set-based semantics. Preserve old bounds and unknown-outcome
handling; a reconciliation candidate does not prove commit and must not trigger an
automatic retry.

### Force-delete with the declaration token

Before:

<!-- orm-example: force-before -->
```go
func legacyForceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := oldhooks.QueryHookMembers().WithTrashed().ForceDelete(ctx, tx, id)
	return err
}
```

After:

<!-- orm-example: force-after -->
```go
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}
```

Every force path requires the token, including ForceDeleteAll, ForceDeleteEach,
ForcePruneBatch, source force deletion and force-detach pivot operations. Zero tokens
and tokens from another declaration fail before SQL. The token does not change query
visibility: retain `WithTrashed()` or `OnlyTrashed()` when required. Normal Delete is
soft deletion for a declared soft-delete model; Restore clears its timestamp.

## Declare and load relations

Keep `relation.One`, `Many`, `Through` and other slots on the model. Bind them inside
Define with `OneOf`, `ManyOf` or the matching slot constructor. Declare relation
variables after both tables exist, replacing `defineXRelations` and generated
relation sets:

Before:

<!-- orm-example: relations-before -->
```go
func legacyRelations(ctx context.Context, db database.Executor) error {
	_, err := old.QueryOrders().With(old.OrderRelations().Buyer).All(ctx, db)
	return err
}
```

After:

<!-- orm-example: relations-after -->
```go
var OrderBuyer = orm.BelongsToOf(models.Orders.Buyer, models.Orders.BuyerID, models.Users.ID)

func relations(ctx context.Context, db database.Executor, existing []models.Order) error {
	_, err := models.Orders.Query().With(OrderBuyer).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = orm.Load(ctx, db, existing, OrderBuyer)
	return err
}
```

`HasManyOf`, `HasOneOf`, many-to-many/pivot, through, morph and one-of-many declarations
follow the same typed slot/key pattern. Existing relationship filters and aggregates
use the declared relation (`orm.Has`, `DoesntHave`, and the typed aggregate helpers).
`With` composes the eager tree; `Load` returns copies of existing parents and
`LoadMissing` preserves already loaded slots. Relations keep target/pivot scopes,
soft-delete visibility, bounded batches and all-or-error attachment.

`LoadFrom`/`LoadMissingFrom` take `(ctx, executor, query, parents)`; the query
supplies its `With` tree and relation limits. Do not replace that behavior with
plain `Load` if the caller needs query-specific limits. The supplied parent slice
still owns its authorization provenance; these helpers do not refetch it.

## Move lifecycle factories and observers

### Write hooks and draft access

Before:

<!-- orm-example: hooks-before -->
```go
func legacyHooks() oldhooks.MemberHooks {
	return oldhooks.MemberHooks{Creating: func(_ context.Context, _ *database.Tx, draft *oldhooks.MemberDraft) error {
		if !draft.Name().IsSet() {
			*draft = draft.SetName("guest")
		}
		return nil
	}}
}
```

After:

<!-- orm-example: hooks-after -->
```go
func memberHooks() orm.Hooks[hooks.Member] {
	return orm.Hooks[hooks.Member]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[hooks.Member]) error {
		if !orm.WriteIsSet(draft, hooks.Members.Name.Set) {
			return orm.WriteSet(draft, hooks.Members.Name.Set("guest"))
		}
		return nil
	}}
}
```

Attach the per-write factory with `orm.WithHooks(memberHooks)` on Define. Keep all
old stages and their ordering: Saving precedes Creating/Updating; Created/Updated
precedes Saved. Deleting/Deleted, Restoring/Restored, ForceDeleting/ForceDeleted and
AfterCommit retain their operation-specific behavior. Local callbacks run before
observer callbacks. Hooks share the effective write; after callbacks receive stored
changes, not speculative input.

Read with `WriteGet(write, column.Set)`, `WriteNullableGet`, or `WriteIsSet`; assign
with `WriteSet`. Unset restores omission rather than assigning NULL or a zero:

Before:

<!-- orm-example: unset-before -->
```go
func legacyUnset(draft *oldhooks.MemberDraft) { *draft = draft.UnsetNote() }
```

After:

<!-- orm-example: unset-after -->
```go
func unset(draft *orm.Write[hooks.Member]) error {
	return orm.WriteUnset(draft, hooks.Members.Note.Set)
}
```

Before:

<!-- orm-example: changes-before -->
```go
func legacyChanges(changes oldhooks.MemberChanges) bool { return changes.Fields().Name.Changed() }
```

After:

<!-- orm-example: changes-after -->
```go
func changes(changes orm.Changes[hooks.Member]) (bool, error) {
	field, err := orm.ChangeOf(changes, hooks.Members.Name)
	return field.Changed(), err
}
```

`Changes.Before`/`After` contain persisted snapshots. `ChangeOf` and
`NullableChangeOf` return `lifecycle.FieldChange`: Assigned means input was supplied;
Changed compares stored values. `orm.Compare(table, before, after, assignments...)`
replaces generated CompareX helpers. Updates use locked originals and RETURNING
capture. Audit uses stored changes, preserving disclosure policies and redaction.

Before/after errors abort the write/savepoint; lifecycle wrappers retain error
identity with safe formatting. AfterCommit runs only after a confirmed outer commit.
Its failure does not roll back an already committed write. Panic/Goexit containment
and transaction ownership remain framework responsibilities.

### Register one observer family per application

Before:

<!-- orm-example: observers-before -->
```go
func legacyObserver() (lifecycle.Declaration, error) {
	return oldhooks.NewMemberObserver("members").Declare(legacyHooks)
}
```

After:

<!-- orm-example: observers-after -->
```go
func observer() (lifecycle.Declaration, error) {
	return orm.NewObserver[hooks.Member]("members").Declare(memberHooks)
}
```

Use `RegisterObserver` with the application's pool key and factory resolver, as in
the application example below. Registrations are instance-owned, including models
without local factories. Do not install old and new adapter families for the same
model. The read-only lifecycle registration-count API exposes no adapters.

### Retrieve after complete hydration

Before:

<!-- orm-example: retrieved-before -->
```go
func legacyRetrieval() oldreads.MemberRetrievalHooks {
	return oldreads.MemberRetrievalHooks{Retrieved: func(context.Context, database.Executor, oldreads.Member) error { return nil }}
}
```

After:

<!-- orm-example: retrieved-after -->
```go
func retrieval() orm.RetrievalHooks[reads.Member] {
	return orm.RetrievalHooks[reads.Member]{Retrieved: func(context.Context, database.Executor, reads.Member) error { return nil }}
}
```

Attach `orm.WithRetrievalHooks(retrieval)` and register retrieval observers with
`NewRetrievalObserver`/`RegisterRetrievalObserver`. The local factory precedes observer
factories for each nonempty complete batch. Each complete model receives Retrieved
after decoding and row closure. Streaming uses closed batches when callbacks exist.
DTO/scalar reads and internal write hydration skip retrieval. Complete-model
`Table.Raw` dispatches retrieval but bypasses global scopes; its SQL must enforce
any required tenant restriction.

## Preserve global scopes and tenancy

Before:

<!-- orm-example: scopes-before -->
```go
func legacyScope() query.GlobalScope[oldreads.Member] {
	return query.NewContextScope[oldreads.Member]("tenant", func(ctx context.Context) (query.Predicate[oldreads.Member], error) {
		id, err := tenantID(ctx)
		return oldreads.MemberFields().TenantID.Eq(id), err
	})
}
```

After:

<!-- orm-example: scopes-after -->
```go
func scope(t reads.MemberTable) orm.GlobalScope[reads.Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[reads.Member], error) {
		id, err := tenantID(ctx)
		return t.TenantID.Eq(id), err
	})
}
```

Attach a lazy immutable source with `orm.WithGlobalScopes(func(t TableType)
[]orm.GlobalScope[Model] { ... })` on Define. The source receives the completed table;
avoid reading a partly initialized package variable. Scope callbacks return additive
predicates and errors. Missing context and callback failures fail closed. Scope-source
failure is sticky; later queries cannot silently run unscoped.

Scopes cover reads, writes, relation targets/pivots, aggregates, joins, subqueries,
cursors and chunks. Inserts are unfiltered. Bypass operations are query-local package
functions:

Before:

<!-- orm-example: bypass-before -->
```go
func legacyBypass() oldreads.MemberQuery {
	return oldreads.QueryReadMembers().WithoutGlobalScope(oldreads.MemberTenant())
}
```

After:

<!-- orm-example: bypass-after -->
```go
func bypass() orm.Query[reads.Member] {
	return orm.WithoutGlobalScope(reads.Members.Query(), reads.MemberTenant(reads.Members))
}
```

`WithoutGlobalScopes` removes all global scopes on that query. Relation and pivot
variants are `WithoutRelationGlobalScope(s)` and `WithoutPivotGlobalScope(s)`.
They do not remove explicit predicates or grant tenant authorization.

## Port conflicts, lookups and source writes

### Conflict targets and typed EXCLUDED

Before:

<!-- orm-example: conflict-before -->
```go
func legacyUpsert(ctx context.Context, tx *database.Tx) error {
	f := old.UserFields()
	policy := query.OnConflict(f.Email).DoUpdate(f.Age.Incoming())
	_, err := old.QueryUsers().Upsert(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test").SetAge(30), policy)
	return err
}
```

After:

<!-- orm-example: conflict-after -->
```go
func upsert(ctx context.Context, tx *database.Tx) error {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Age))
	_, err := orm.Upsert(ctx, tx, f.Query(), policy, f.Email.Set("ada@example.test"), f.Age.Set(30))
	return err
}
```

Before:

<!-- orm-example: excluded-before -->
```go
func legacyExcluded() query.Conflict[old.User] {
	q := old.QueryUsers()
	r := q.ConflictRows()
	stored, proposed := old.UserFieldsAt(r.Stored()), old.UserFieldsAt(r.Proposed())
	return query.OnConflict(old.UserFields().Email).DoUpdate(
		query.SetConflictValue(old.UserFields().Age, query.Add(stored.Age, proposed.Age)))
}
```

After:

<!-- orm-example: excluded-after -->
```go
func excluded() orm.Conflict[models.User] {
	r := rows.ConflictRowsOf(models.Users.Query())
	stored := expr.At(r.Stored(), models.Users.Age)
	proposed := expr.At(r.Proposed(), models.Users.Age)
	return orm.OnConflict(orm.ConflictKey(models.Users.Email)).DoUpdate(
		rows.SetConflictValue(models.Users.Age, expr.Add(stored, proposed)))
}
```

Use `OnConflictConstraint[M](name)` for a named constraint and `DoNothing()` for
DO NOTHING. `ConflictKey`, `ConflictSet`, `ConflictNull` and `ConflictIncoming`
replace generated conflict-field methods. ConflictSet takes the mutation input type;
Incoming/Proposed represent normalized stored EXCLUDED values, including defaults
and trigger effects. `rows.OnConflictKeys`, `ConflictTargetWhere` and
`ConflictWhereRows` handle expression/partial targets and stored/proposed conditions.
Preserve physical unique constraints; application lookup logic cannot replace them.

### Lookup writes

Before:

<!-- orm-example: lookup-before -->
```go
func legacyLookup(ctx context.Context, tx *database.Tx) error {
	_, err := old.QueryUsers().Where(old.UserFields().Email.Eq("ada@example.test")).FirstOrCreate(ctx, tx, old.UserDraft{})
	return err
}
```

After:

<!-- orm-example: lookup-after -->
```go
func lookup(ctx context.Context, tx *database.Tx) error {
	_, err := orm.FirstOrCreate(ctx, tx, models.Users.Query().Where(models.Users.Email.Eq("ada@example.test")))
	return err
}
```

`orm.UpdateOrCreate` takes separate creation assignments and an update callback
returning assignments. `orm.CreateOrFirst` replaces the old insertion-first
`InsertOrFirst`: insertion runs in a savepoint, and only an own-table unique violation
with matching schema/table/constraint provenance permits rollback and competitor
lookup. Missing provenance and uncertain outcomes fail closed. Internal reads skip
Retrieved. These operations do not automatically retry.

### Insert-select and source update/delete

Before:

<!-- orm-example: source-insert-before -->
```go
func legacyInsertSelect(ctx context.Context, tx *database.Tx) error {
	f := oldhooks.MemberFields()
	plan := oldhooks.InsertMemberFrom(oldhooks.QueryHookMembers()).
		SelectID(query.Value(f.ID)).SelectBody(query.Value(f.Body)).SelectCreatedAt(query.Value(f.CreatedAt)).
		Values(oldhooks.MemberDraft{}.SetName("copy"))
	_, err := plan.Exec(ctx, tx)
	return err
}
```

After:

<!-- orm-example: source-insert-after -->
```go
func insertSelect(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	plan := rows.InsertFrom(rows.From(f.Query()), f.Query(),
		rows.MapInsert(f.ID, expr.Value(f.Key())), rows.MapInsert(f.Body, expr.Value(f.Body)),
		rows.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt))).Values(f.Name.Set("copy"))
	_, err := plan.Exec(ctx, tx)
	return err
}
```

Before:

<!-- orm-example: source-update-before -->
```go
func legacyUpdateFrom(ctx context.Context, tx *database.Tx) error {
	f, q := oldhooks.MemberFields(), oldhooks.QueryHookMembers()
	_, err := oldhooks.UpdateMemberFrom(q, q).MatchID(query.Value(f.ID)).
		SelectBody(query.Value(f.Body)).Values(oldhooks.MemberDraft{}.SetName("updated")).Exec(ctx, tx)
	return err
}
```

After:

<!-- orm-example: source-update-after -->
```go
func updateFrom(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	_, err := rows.UpdateFrom(f.Query(), rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).Values(f.Name.Set("updated")).Exec(ctx, tx)
	return err
}
```

Before:

<!-- orm-example: source-delete-before -->
```go
func legacyDeleteUsing(ctx context.Context, tx *database.Tx) error {
	q := oldhooks.QueryHookMembers().WithTrashed()
	_, err := oldhooks.ForceDeleteMemberUsing(q, q).MatchID(query.Value(oldhooks.MemberFields().ID)).Exec(ctx, tx)
	return err
}
```

After:

<!-- orm-example: source-delete-after -->
```go
func deleteUsing(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	q := f.Query().WithTrashed()
	_, err := rows.ForceDeleteUsing(q, f.Soft, rows.From(q)).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).Exec(ctx, tx)
	return err
}
```

Use `DeleteUsing` for ordinary/soft deletion and `ForceDeleteUsing` with the soft
capability for physical deletion. `MatchNullableSource` models nullable match keys;
nullable source assignments still cannot fill required destinations. Input-mutated
fields go through typed literal `Values`, not source mappings. Keep returning bounds
on `.Returning(ctx, db, limit)`. These are set operations, not per-model lifecycle
loops; they retain the old source-write convention and callback policy.

## Separate SQL rows from public DTOs

### Scalars, projections and DTO ownership

Before:

<!-- orm-example: scalar-before -->
```go
func legacyScalar(ctx context.Context, db database.Executor) error {
	_, err := query.SelectValue(old.QueryOrders(), query.Value(old.OrderFields().TotalCents)).All(ctx, db)
	return err
}
```

After:

<!-- orm-example: scalar-after -->
```go
func scalar(ctx context.Context, db database.Executor) error {
	_, err := rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents).All(ctx, db)
	return err
}
```

Before:

<!-- orm-example: projection-before -->
```go
func legacyReport() query.ProjectionQuery[reports.Member, reports.MemberRow] {
	f := reports.MemberFields()
	return reports.ProjectMemberRow(reports.QueryReportMembers()).
		SelectID(query.Value(f.ID)).SelectName(query.Value(f.Name)).SelectNickname(query.Value(f.Nickname)).
		SelectState(query.Value(f.State)).SelectBalance(query.Value(f.Balance)).Query()
}
```

After:

<!-- orm-example: projection-after -->
```go
func report() rows.Query[reports.Member, reports.MemberRow] {
	f, r := reports.Members, reports.MemberRows
	return rows.Select(rows.From(f.Query()), r,
		rows.Map(r.ID, expr.Value(f.ID)), rows.Map(r.Name, expr.Value(f.Name)),
		rows.Map(r.Nickname, expr.Value(f.Nickname)), rows.Map(r.State, expr.Value(f.State)),
		rows.Map(r.Balance, expr.Value(f.Balance)))
}
```

Declare each concrete row with `rows.Define`, embed `rows.Row[R]` from `b.Row()`, and
bind fields with rows constructors. `rows.Map` matches the exact projected type;
`MapSelected` accepts a selected expression. The existing
[row declaration fixture](../../tests/fixtures/goway/reporting/members.go) shows the
complete MemberRows schema used above. All fields must be mapped exactly once.

A SQL row is not implicitly an API response. Replace `//foundry:projection dto=true`
with a handwritten rows declaration plus retained `//foundry:dto` generation at the
serial barrier. Preserve the DTO name, JSON tags, validation rules and descriptors;
DTO/client generation still runs. Plain internal projections need no DTO annotation.
Do not expose persistence models as response DTOs or change contract bytes to make
migration easier. Persistence models are recognized from `orm.Table` and `orm.Builder`
signatures across containers, methods, interfaces, generic type arguments, and direct
`orm.Define` calls; avoid erasing table handles behind `any` or untyped wrappers.

### Aggregates, windows and CTEs

Before:

<!-- orm-example: aggregate-before -->
```go
func legacyAggregate(ctx context.Context, db database.Executor) error {
	count := query.Count[old.Order]()
	q := query.SelectValue(old.QueryOrders(), count.Value()).
		GroupBy(old.OrderFields().BuyerID.Group()).Having(count.Gt(1))
	_, err := q.All(ctx, db)
	return err
}
```

After:

<!-- orm-example: aggregate-after -->
```go
func aggregate(ctx context.Context, db database.Executor) error {
	count := expr.Count[models.Order]()
	q := rows.GroupByScalar(rows.SelectScalar(rows.From(models.Orders.Query()), count.Value()),
		expr.Group(expr.Value(models.Orders.BuyerID)))
	_, err := rows.ReadScalar(rows.HavingScalar(q, count.Gt(1))).All(ctx, db)
	return err
}
```

Before:

<!-- orm-example: window-before -->
```go
func legacyWindow(ctx context.Context, db database.Executor) error {
	a := query.As[orderSide](old.QueryOrders(), "o")
	f := old.OrderFieldsAt(a.Scope())
	window := query.WindowFor(a).PartitionBy(f.BuyerID.Group()).OrderBy(f.TotalCents.Desc())
	_, err := query.SelectValue(a, query.RowNumber(window)).All(ctx, db)
	return err
}
```

After:

<!-- orm-example: window-after -->
```go
func window(ctx context.Context, db database.Executor) error {
	a := rows.As[orderSide](models.Orders.Query(), "o")
	s := rows.Scope(a)
	window := expr.WindowFor(s).PartitionBy(expr.Group(expr.At(s, models.Orders.BuyerID))).
		OrderBy(expr.At(s, models.Orders.TotalCents).Desc())
	_, err := rows.ReadScalar(rows.SelectScalar(a, expr.RowNumber(window))).All(ctx, db)
	return err
}
```

Before:

<!-- orm-example: cte-before -->
```go
func legacyCTE(ctx context.Context, db database.Executor) error {
	cte := query.CTE("eligible", old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100)))
	a := query.As[orderSide](cte, "o")
	_, err := query.SelectRecord(a, a.Scope()).All(ctx, db)
	return err
}
```

After:

<!-- orm-example: cte-after -->
```go
func cte(ctx context.Context, db database.Executor) error {
	cte := rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(100))))
	a := rows.AsRecord[orderSide](cte, "o")
	_, err := rows.SelectRecord(a, rows.Scope(a)).All(ctx, db)
	return err
}
```

Use `expr.Sum`, Avg/Min/Max and scoped variants for aggregates. `rows.GroupBy`/Having
operate on row queries; `GroupByScalar`/HavingScalar operate on scalar selections.
The expression types preserve row, aggregate and window phases. Window frames retain
Rows/Range/Groups bounds and exclusions. Nullable aggregate output stays nullable.

CTEs wrap complete record sources (`ModelRecord`, `RecordOf`); alias them with
`AsRecord`. Recursive CTEs use typed RecursiveSelf callbacks. Locked CTEs use
TransactionCTE/AsTransaction and continue to require a transaction. Joins use
`rows.As`, `expr.At`, `rows.On` and LeftScope/RightScope; move expressions into the
joined scope instead of reusing an unrelated alias.

### DISTINCT and set operations

Before:

<!-- orm-example: sets-before -->
```go
func legacySets(ctx context.Context, db database.Executor) error {
	a := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100))
	_, err := query.Union(a, old.QueryOrders()).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Distinct().All(ctx, db)
	return err
}
```

After:

<!-- orm-example: sets-after -->
```go
func sets(ctx context.Context, db database.Executor) error {
	a := models.Orders.Query().Where(models.Orders.TotalCents.Gt(100))
	_, err := rows.Union(rows.ModelRecord(a), rows.ModelRecord(models.Orders.Query())).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = rows.Distinct(rows.FromModel(models.Orders.Query())).All(ctx, db)
	return err
}
```

`rows.FromModel` is the complete-model read adapter for ordinary row clauses.
Set operations preserve complete record types. Use the corresponding value/result
adapters for scalar or projected rows; read-only composition does not acquire writes.
`rows.EachRead` streams bounded complete-record batches; `rows.Each` iterates decoded
DTO rows. Retain the existing callback/row-closure guarantees when switching families.

### Typed JSON paths

Before:

<!-- orm-example: json-before -->
```go
func legacyJSON() query.NullableTextRowExpression[jsonpaths.Document, string] {
	field := query.NewJSONField[jsonpaths.Document, value.JSON[jsonpaths.Preferences]]("json_documents", "settings", codec.JSON[jsonpaths.Preferences]())
	theme := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Preferences, string](query.JSONRoot(field), "theme", false)
	return query.JSONTextScalar(theme, codec.String[string]())
}
```

After:

<!-- orm-example: json-after -->
```go
var Theme = expr.JSONFieldOf(func(p *jsonpaths.Preferences) *value.Optional[string] { return &p.Theme })

func jsonTheme() expr.NullableTextRowExpression[jsonpaths.Document, string] {
	root := expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
	theme := expr.JSONOptional(expr.JSONProperty(root, Theme))
	return expr.JSONTextScalar(theme, codec.String[string]())
}
```

The old example shows the runtime path behind generated JSON selectors. Declare
native properties with `expr.JSONFieldOf` selectors and validate them at startup.
JSON tags, optional/null/pointer structure and `,string` semantics drive the path.
Use JSONOptional/JSONPointer, map-key and array helpers for nested shapes. Distinguish
SQL NULL from JSON null and absent properties. Field paths are bound data, not SQL
string interpolation. Model JSON path generation retires; DTO naming and validation
semantics remain.

## Keep datatable authorization and wire contracts

The old Source callback returned a generated ProjectionQuery. The new Spec expects
`rows.Query[S,R]`; columns and filters use phase-appropriate expr values:

Before:

<!-- orm-example: datatable-before -->
```go
func legacyTableSource(ctx context.Context, actor reports.Authority, tenant func(context.Context, reports.Authority) (int64, error)) (query.ProjectionQuery[reports.Member, reports.MemberRow], error) {
	id, err := tenant(ctx, actor)
	if err != nil {
		return query.ProjectionQuery[reports.Member, reports.MemberRow]{}, err
	}
	return legacyReport().Where(reports.MemberFields().TenantID.Eq(id)), nil
}
```

After:

<!-- orm-example: datatable-after -->
```go
func tableSource[A any](spec datatable.Spec[reports.Member, reports.MemberRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[reports.Member, reports.MemberRow, A] {
	// Keep the existing Spec authorization; tenant resolves the trusted actor scope.
	spec.Source = func(ctx context.Context, actor A) (rows.Query[reports.Member, reports.MemberRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[reports.Member, reports.MemberRow]{}, err
		}
		return report().Where(expr.Value(reports.Members.TenantID).Eq(id)), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Value(reports.Members.ID).Asc()}
	return spec
}
```

Pass an authorized, tenant-constrained native source and retain the existing Spec
ID, DTO descriptor, columns, labels, filters and export capabilities. Column
`SortBy` accepts `expr.Select(expr.Value(Members.Name))` or comparison `.Value()`
for enums (`expr.CompareRow(expr.Value(Members.State)).Value()`), while `Stable`
takes the short `expr.Value(MemberModels.ID).Asc()` spelling. The complete
[datatable fixture](../../tests/fixtures/consumer/reporting/members.go) uses
`rows.Select`, `expr.Select`, `expr.TextRow`, nullable filters and typed related filters.
Do not bypass actor resolution or reconciliation to adapt a
source signature. Manifests, OpenAPI, TypeScript and filter/response bytes stay fixed.

### Port the application boundaries from the pilot

The [datatable guide](datatable.md) describes request operators, authorization and
exports. Use this table when replacing a generated field namespace `f` with an
ORM declaration. `RowEquality` and `RowNullable` are interfaces, not constructors;
`datatable.Where` and `NullableWhere` still construct the filters.

| Before | After |
| --- | --- |
| `SortBy(query.Value(f.X))` | `SortBy(expr.Select(expr.Value(f.X)))`, with `f` now the ORM declaration. The selected value must match the DTO field type, including nullability. |
| `datatable.Where(text, f.Username)` | `datatable.Where(text, expr.TextRow(f.Username))`. |
| `datatable.NullableWhere(text, f.Email)` | `datatable.NullableWhere(text, expr.TextNullableRow(f.Email))`. |
| Equality-only or enum `datatable.Where(codec, f.X)` | `datatable.Where(codec, expr.CompareRow(expr.Value(f.X)))`; use `CompareNullableRow` with `NullableWhere`. |
| Ordered numeric `datatable.Where(codec, f.X)` | `datatable.Where(codec, expr.OrderRow(expr.Value(f.X)))`; use `OrderNullableRow` for nullable values. Choose by the column's declared capabilities, not its underlying Go type. |
| `Stable: []query.ProjectionOrder[M]{f.ID.Asc()}` | `Stable: []expr.ProjectionOrder[M]{expr.Value(f.ID).Asc()}`. Use `f.Key()` when the declaration has no named ID carrier. |
| `query.ProjectionQuery[M,R]`, `SelectXRow(source, XRowSelection[M]{...})` | `rows.Query[M,R]`, `rows.Select(rows.From(source), XRows, rows.Map(...))`. Use `MapSelected` for computed selected values. |
| Joined `query.Value(joinedField)` sort | `expr.Select(expr.At(joinedScope, field))`. The Spec's first type parameter is the joined scope, not one model. |
| Correlated `CorrelatedScalarRowQuery(...).Value()` sort | `expr.Select(rows.CorrelatedScalarRowQuery(...))`; use `CorrelatedScalarNullableRowQuery` when the inner value is already nullable. |
| `datatable.Having` / `NullableHaving` | Keep these constructors; supply native aggregates such as `expr.Count[M]()` / `expr.Sum(f.Amount)`, which return group predicates. |
| `datatable.Related(child, exists)` | Keep `Related`; its callback now maps `expr.Predicate[Child]` to `expr.Predicate[Parent]`, for example with `expr.Has`. |

Text adapters retain text operators. Equality adapters preserve enum restrictions.
Keep each column's existing operator allowlist and `.Restrict(...)` calls. LIKE is
still an explicit `.AllowLike()` opt-in; `Contains` and `IContains` treat input as
literal text. Nullable negation includes NULL rows. Mixed WHERE/HAVING AND is
supported, but an OR or NOT spanning both phases is rejected before I/O.

For nullable enums, nullable numbers, non-nullable correlated subquery sorts and
outer joins, compose the adapters as shown below:

<!-- orm-example: pilot-left-join-adapters -->
```go
type auditAdminSide struct{}
type auditCountrySide struct{}
type AuditScope = rows.Left[rows.Alias[auditAdminSide, Admin], rows.Alias[auditCountrySide, countries.Country]]
type AuditRow struct {
	ID       int64                            `json:"id"`
	Country  value.Nullable[string]           `json:"country"`
	Status   value.Nullable[countries.Status] `json:"status"`
	Latitude value.Nullable[float64]          `json:"latitude"`
}
type AuditColumns struct {
	rows.Row[AuditRow]
	ID       rows.Field[AuditRow, int64]
	Country  rows.Field[AuditRow, value.Nullable[string]]
	Status   rows.Field[AuditRow, value.Nullable[countries.Status]]
	Latitude rows.Field[AuditRow, value.Nullable[float64]]
}

var AuditRows = rows.Define(func(b rows.Builder[AuditRow], r *AuditRow) AuditColumns {
	return AuditColumns{
		b.Row(),
		rows.NumberOf(b, &r.ID),
		rows.NullableTextOf(b, &r.Country),
		rows.NullableEnumOf(b, &r.Status),
		rows.NullableNumberOf(b, &r.Latitude),
	}
})

func auditTable[A any](spec datatable.Spec[AuditScope, AuditRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[AuditScope, AuditRow, A] {
	a := rows.As[auditAdminSide](Admins.Query(), "a")
	c := rows.As[auditCountrySide](countries.Countries.Query(), "c")
	as, cs := rows.Scope(a), rows.Scope(c)
	joined := rows.LeftJoin(a, c, rows.On(expr.At(as, Admins.CountryCode), expr.At(cs, countries.Countries.ISO2)))
	admin := rows.LeftScope(joined, as)
	country := rows.NullableRightScope(joined, cs)
	correlateName := func() rows.CorrelatedScalar[AuditScope, countries.Country, string] {
		q := countries.Countries.Query()
		correlation := rows.Correlate(joined, rows.From(q))
		outer := rows.OuterScope(correlation, admin)
		inner := rows.InnerScope(correlation, rows.ModelScope(q))
		return rows.SelectCorrelatedScalar(correlation, expr.Select(expr.At(inner, countries.Countries.Name))).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.At(inner, countries.Countries.ISO2).Asc()).Limit(1)
	}
	sortedCountry := expr.Select(rows.CorrelatedScalarRowQuery(correlateName()))
	statusFilter := datatable.NullableWhere(
		foundryhttp.EnumQuery[countries.Status, *countries.Status](countries.EnabledStatus.EnumDescriptor()),
		expr.CompareNullableRow(expr.AtNullable(country, countries.Countries.Status)))
	latitudeFilter := datatable.NullableWhere(
		foundryhttp.FloatQuery[float64](),
		expr.OrderNullableRow(expr.AtNullableValue(country, countries.Countries.Latitude)))
	base := rows.Select(joined, AuditRows,
		rows.Map(AuditRows.ID, expr.At(admin, Admins.Key())),
		rows.MapSelected(AuditRows.Country, sortedCountry),
		rows.Map(AuditRows.Status, expr.AtNullable(country, countries.Countries.Status)),
		rows.Map(AuditRows.Latitude, expr.AtNullableValue(country, countries.Countries.Latitude)))
	spec.Columns = []datatable.ColumnRegistration[AuditScope, AuditRow]{
		datatable.DefineColumn[AuditScope](validation.DefineField("country", func(r AuditRow) value.Nullable[string] { return r.Country }), "audit.country").
			SortBy(sortedCountry).Registration(),
		datatable.DefineColumn[AuditScope](validation.DefineField("status", func(r AuditRow) value.Nullable[countries.Status] { return r.Status }), "audit.status").
			FilterBy(statusFilter).Registration(),
		datatable.DefineColumn[AuditScope](validation.DefineField("latitude", func(r AuditRow) value.Nullable[float64] { return r.Latitude }), "audit.latitude").
			FilterBy(latitudeFilter).Registration(),
	}
	spec.Stable = []expr.ProjectionOrder[AuditScope]{expr.At(admin, Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[AuditScope, AuditRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[AuditScope, AuditRow]{}, err
		}
		return base.Where(expr.At(admin, Admins.TenantID).Eq(id)), nil
	}
	return spec
}
```

### Replace the projection declaration and Source callback

Before, documentation only:

```text
//foundry:projection dto=true
type AdminRow struct { /* existing DTO fields and tags */ }
Source: func(ctx context.Context, actor AdminAuthority) (query.ProjectionQuery[models.Admin, AdminRow], error) {
    source := models.QueryAdmins()
    if archived { source = source.OnlyTrashed() }
    return SelectAdminRow(source, AdminRowSelection[models.Admin]{
        ID: query.Value(f.ID), Username: query.Value(f.Username), Email: query.Value(f.Email),
    }), nil
},
```

Keep `AdminRow` at its original package path. Retain public DTO generation with
`//foundry:dto` in place of the projection directive, including its original JSON
names, validation declarations and response descriptor. Add the separate handwritten
rows schema. Regenerate retained DTO output through the Makefile; do not edit it.
The standalone example below uses explicit validation fields for its columns. An
application can keep its existing generated DTO validation fields instead.

<!-- orm-example: pilot-declarations -->
```go
type Admin struct {
	ID          int64
	TenantID    int64
	Username    string
	Email       value.Nullable[string]
	CountryCode countries.Code
	DeletedAt   value.Nullable[time.Time]
}
type AdminTable struct {
	orm.Table[Admin, int64]
	TenantID    orm.Number[Admin, int64]
	Username    orm.Text[Admin, string]
	Email       orm.NullableText[Admin, string]
	CountryCode orm.Column[Admin, countries.Code]
	DeletedAt   orm.SoftDelete[Admin, time.Time]
}

var Admins = orm.Define("admins", func(b orm.Builder[Admin], a *Admin) AdminTable {
	return AdminTable{orm.TableOf(b, &a.ID), orm.NumberOf(b, &a.TenantID),
		orm.TextOf(b, &a.Username), orm.NullableTextOf(b, &a.Email),
		orm.ColumnOf(b, &a.CountryCode), orm.SoftDeleteOf(b, &a.DeletedAt)}
})

// Keep this DTO's original package, name, field types and JSON tags.
type AdminRow struct {
	ID       int64                  `json:"id"`
	Username string                 `json:"username"`
	Email    value.Nullable[string] `json:"email"`
}
type AdminRowColumns struct {
	rows.Row[AdminRow]
	ID       rows.Field[AdminRow, int64]
	Username rows.Field[AdminRow, string]
	Email    rows.Field[AdminRow, value.Nullable[string]]
}

var AdminRows = rows.Define(func(b rows.Builder[AdminRow], r *AdminRow) AdminRowColumns {
	return AdminRowColumns{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Username), rows.NullableTextOf(b, &r.Email)}
})
```

The column registrations now use selected expressions for sorts and row adapters
for filters. Keep the application's other registrations, labels and export codecs.

<!-- orm-example: pilot-columns -->
```go
func adminColumns() []datatable.ColumnRegistration[Admin, AdminRow] {
	text := foundryhttp.StringQuery[string]()
	username := validation.DefineField("username", func(r AdminRow) string { return r.Username })
	email := validation.DefineField("email", func(r AdminRow) value.Nullable[string] { return r.Email })
	return []datatable.ColumnRegistration[Admin, AdminRow]{
		datatable.DefineColumn[Admin](username, "admins.fields.username").
			SortBy(expr.Select(expr.Value(Admins.Username))).
			FilterBy(datatable.Where(text, expr.TextRow(Admins.Username))).
			Searchable().ExportAs(datatable.ScalarCell(text)).Registration(),
		datatable.DefineColumn[Admin](email, "admins.fields.email").
			SortBy(expr.Select(expr.Value(Admins.Email))).
			FilterBy(datatable.NullableWhere(text, expr.TextNullableRow(Admins.Email))).
			Searchable().ExportAs(datatable.NullableCell(text)).Registration(),
	}
}
```

Apply mandatory tenant and visibility scopes before selecting the row. `OnlyTrashed`
and `WithTrashed` remain model-query operations and survive `rows.From` and
`rows.Select`. The callback below receives an existing Spec so its authorization
and DTO descriptor remain caller-owned. The tenant resolver must use trusted actor
context. A UI filter must not replace that resolver.

<!-- orm-example: pilot-source -->
```go
func adminSource(tenantID int64, archived bool) rows.Query[Admin, AdminRow] {
	source := Admins.Query().Where(Admins.TenantID.Eq(tenantID))
	if archived {
		source = source.OnlyTrashed()
	}
	return rows.Select(rows.From(source), AdminRows,
		rows.Map(AdminRows.ID, expr.Value(Admins.Key())),
		rows.Map(AdminRows.Username, expr.Value(Admins.Username)),
		rows.Map(AdminRows.Email, expr.Value(Admins.Email)))
}

func adminTable[A any](spec datatable.Spec[Admin, AdminRow, A], tenant func(context.Context, A) (int64, error), archived bool) datatable.Spec[Admin, AdminRow, A] {
	// Retain the caller's Row descriptor, Authorize, ID, labels and export policy.
	spec.Columns = adminColumns()
	spec.Stable = []expr.ProjectionOrder[Admin]{expr.Value(Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[Admin, AdminRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[Admin, AdminRow]{}, err
		}
		return adminSource(id, archived), nil
	}
	return spec
}
```

Do not paginate or collect Source results yourself. The datatable owns pagination,
report ordering and streaming export execution. Stable orders must uniquely identify
the result; a joined or grouped result may need more than its parent ID.

### Keep group and relation filters in their phases

`Count` returns a required `int64`. Exact numeric `Sum` returns nullable
`decimal.Decimal`, including for integer input. Pass the present-value decimal
codec to `NullableHaving`, which supplies null operations. Keep the source's
GROUP BY and aggregate mappings; these filter constructors do not add grouping.
The [aggregate example above](#aggregates-windows-and-ctes) shows selected mappings.

<!-- orm-example: pilot-filter-families -->
```go
func orderGroupFilters() (datatable.FilterSource[models.Order, int64], datatable.FilterSource[models.Order, value.Nullable[decimal.Decimal]]) {
	count := expr.Count[models.Order]()
	total := expr.Sum(models.Orders.TotalCents)
	return datatable.Having(foundryhttp.IntegerQuery[int64](), count),
		datatable.NullableHaving(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), total)
}

func relatedOrderFilter() datatable.FilterSource[models.User, int64] {
	return datatable.Related(
		datatable.Where(foundryhttp.IntegerQuery[int64](), expr.OrderRow(expr.Value(models.Orders.TotalCents))),
		func(p expr.Predicate[models.Order]) expr.Predicate[models.User] {
			return expr.Has(models.UserRelations.Orders, p)
		})
}

func statusFilter() datatable.FilterSource[models.User, models.Status] {
	return datatable.Where(foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()),
		expr.CompareRow(expr.Value(models.Users.Status)))
}
```

`Related` accepts only a child WHERE filter, not HAVING. The declared relation keeps
its owner and target scopes. Its negation is NOT EXISTS. A custom callback must
return a predicate in the enclosing source scope; it cannot return an unrelated
model's predicate or turn a group predicate into a row predicate.

### Bind joined sorts and correlated filters to their source

This worked example joins administrators to their country and selects an optional
official name with a correlated scalar query. It uses the existing countries table
rather than a generated field namespace. Keep any additional authorization scopes
from your application on each joined source.

<!-- orm-example: pilot-joined-schema -->
```go
type adminSide struct{}
type countrySide struct{}
type DirectoryScope = rows.Inner[rows.Alias[adminSide, Admin], rows.Alias[countrySide, countries.Country]]
type DirectoryRow struct {
	ID           int64                  `json:"id"`
	Country      string                 `json:"country"`
	OfficialName value.Nullable[string] `json:"officialName"`
}
type DirectoryColumns struct {
	rows.Row[DirectoryRow]
	ID           rows.Field[DirectoryRow, int64]
	Country      rows.Field[DirectoryRow, string]
	OfficialName rows.Field[DirectoryRow, value.Nullable[string]]
}

var DirectoryRows = rows.Define(func(b rows.Builder[DirectoryRow], r *DirectoryRow) DirectoryColumns {
	return DirectoryColumns{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Country), rows.NullableTextOf(b, &r.OfficialName)}
})
```

A custom nullable adapter implements `Eq`, `Ne`, `In`, `IsNull` and `IsNotNull`,
returning `expr.Predicate[S]`. Optional methods add operators; this adapter also
supports `IContains`. Here a missing country or a NULL official name counts as null.
Each comparison uses EXISTS over the matching country. The unique country key
ensures the filtered value and the sorted scalar refer to the same row.

<!-- orm-example: pilot-custom-filter -->
```go
type officialNameFilter[S any] struct {
	exists func(orm.Predicate[countries.Country]) expr.Predicate[S]
}

func (f officialNameFilter[S]) Eq(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.Eq(v))
}
func (f officialNameFilter[S]) Ne(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.Ne(v))
}
func (f officialNameFilter[S]) In(v ...string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.In(v...))
}
func (f officialNameFilter[S]) IContains(v string) expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.IContains(v))
}
func (f officialNameFilter[S]) IsNull() expr.Predicate[S] {
	return expr.Not(f.IsNotNull())
}
func (f officialNameFilter[S]) IsNotNull() expr.Predicate[S] {
	return f.exists(countries.Countries.OfficialName.IsNotNull())
}
```

`As` creates the SQL aliases. An aliased `Admins.Query()` keeps its soft-delete
visibility, and `.OnlyTrashed()`/`.WithTrashed()` applies before `rows.As`. Lift
fields with `LeftScope`/`RightScope` after the join, then with `OuterScope`/`InnerScope`
inside the correlation. Keep its ORDER BY and LIMIT inside the scalar subquery.
Selecting an already nullable value uses the nullable scalar adapter, so the
output has one nullable layer.

<!-- orm-example: pilot-joined-source -->
```go
func directoryTable[A any](spec datatable.Spec[DirectoryScope, DirectoryRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[DirectoryScope, DirectoryRow, A] {
	a := rows.As[adminSide](Admins.Query(), "a")
	c := rows.As[countrySide](countries.Countries.Query(), "c")
	as, cs := rows.Scope(a), rows.Scope(c)
	joined := rows.InnerJoin(a, c, rows.On(expr.At(as, Admins.CountryCode), expr.At(cs, countries.Countries.ISO2)))
	admin, country := rows.LeftScope(joined, as), rows.RightScope(joined, cs)
	correlate := func(ps ...orm.Predicate[countries.Country]) rows.CorrelatedScalar[DirectoryScope, countries.Country, value.Nullable[string]] {
		q := countries.Countries.Query().Where(ps...)
		correlation := rows.Correlate(joined, rows.From(q))
		outer := rows.OuterScope(correlation, admin)
		inner := rows.InnerScope(correlation, rows.ModelScope(q))
		return rows.SelectCorrelatedScalar(correlation, expr.Select(expr.At(inner, countries.Countries.OfficialName))).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.At(inner, countries.Countries.ISO2).Asc()).Limit(1)
	}
	selected := expr.Select(rows.CorrelatedScalarNullableRowQuery(correlate()))
	filter := officialNameFilter[DirectoryScope]{exists: func(p orm.Predicate[countries.Country]) expr.Predicate[DirectoryScope] {
		return correlate(p).Exists()
	}}
	base := rows.Select(joined, DirectoryRows,
		rows.Map(DirectoryRows.ID, expr.At(admin, Admins.Key())),
		rows.Map(DirectoryRows.Country, expr.At(country, countries.Countries.Name)),
		rows.MapSelected(DirectoryRows.OfficialName, selected))
	text := foundryhttp.StringQuery[string]()
	spec.Columns = []datatable.ColumnRegistration[DirectoryScope, DirectoryRow]{
		datatable.DefineColumn[DirectoryScope](validation.DefineField("country", func(r DirectoryRow) string { return r.Country }), "admins.country").
			SortBy(expr.Select(expr.At(country, countries.Countries.Name))).
			FilterBy(datatable.Where(text, expr.TextRowAt(country, countries.Countries.Name))).Registration(),
		datatable.DefineColumn[DirectoryScope](validation.DefineField("officialName", func(r DirectoryRow) value.Nullable[string] { return r.OfficialName }), "admins.officialName").
			SortBy(selected).FilterBy(datatable.NullableWhere(text, filter)).Registration(),
	}
	spec.Stable = []expr.ProjectionOrder[DirectoryScope]{expr.At(admin, Admins.Key()).Asc()}
	spec.Source = func(ctx context.Context, actor A) (rows.Query[DirectoryScope, DirectoryRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[DirectoryScope, DirectoryRow]{}, err
		}
		return base.Where(expr.At(admin, Admins.TenantID).Eq(id)), nil
	}
	return spec
}
```

For a LEFT JOIN, replace the right preserved scope with
`rows.NullableRightScope(joined, rightScope)` and read it with `expr.AtNullable` for
required fields or `expr.AtNullableValue` for already nullable fields. Change the
DTO and Spec scope accordingly; do not cast the INNER scope or keep a required DTO
field for an optional joined value. See [joins](model-joins.md) for the compiled
outer-join examples. The worked INNER JOIN excludes administrators without a
matching country; its official name can still be SQL NULL.

### Replace Foundry Countries factories and drafts

See [Countries](countries.md) for seeding, reference-data ownership and bounds.
The concrete `countries.Country`, `countries.Code` and `countries.Status` identities
remain unchanged. Import aliases such as `reference` do not change the mapping.

| Before | After |
| --- | --- |
| `reference.QueryFoundryCountries()` | `reference.Countries.Query()`. |
| `reference.CountryFields()` | `reference.Countries`. |
| `QueryFoundryCountries().Find(ctx, db, code)` | `Countries.Find(ctx, db, code)`. |
| `CountryDraft{}.SetStatus(status).SetConversionRate(rate)` | Separate assignments `Countries.Status.Set(status)`, `Countries.ConversionRate.Set(rate)`. |
| `QueryFoundryCountries().Update(ctx, tx, code, draft)` | `Countries.Update(ctx, tx, code, assignments...)`. |

<!-- orm-example: pilot-countries -->
```go
func countryRead(ctx context.Context, db *database.DB, code countries.Code) (value.Optional[countries.Country], error) {
	country, err := countries.Countries.Find(ctx, db, code)
	if err != nil || !country.IsSet() {
		return country, err
	}
	f := countries.Countries
	_, err = f.Query().Where(f.Status.Eq(countries.EnabledStatus)).
		OrderBy(f.Name.Asc(), f.ISO2.Asc()).Limit(countries.MaxCountries).All(ctx, db.Primary())
	return country, err
}

func countryWrite(ctx context.Context, tx *database.Tx, code countries.Code, rate decimal.Decimal) error {
	f := countries.Countries
	_, err := f.Query().Where(f.ISO2.Eq(code)).ForUpdate().First(ctx, tx)
	if err != nil {
		return err
	}
	_, err = f.Update(ctx, tx, code, f.Status.Set(countries.EnabledStatus), f.ConversionRate.Set(rate))
	return err
}
```

Keep the caller-owned transaction for the locking read and update. `Find` and
`First` retain their optional-result contract (`value.Optional`); inspect or
return the found result if application behavior depends on absence. `Update`
reports a missing row rather than inserting it. An omitted conversion-rate
assignment leaves it unchanged; `SetNull()` explicitly clears it. The filtered
list retains the primary executor, stable name/code ordering and `MaxCountries`
bound from the old call.

### Port the generic validation lookup boundary

Before, documentation only:

```text
func UniqueUsername[M any](executor database.Executor, source query.ModelQuerySource[M], column query.KeyField[M, string]) validation.Rule[string]
// caller: UniqueUsername(tx, models.QueryAdmins().WithTrashed(), models.AdminFields().Username)
```

The native source is `orm.Query[M]`; the field parameter is
`orm.KeyColumn[M,string]`. `KeyColumn` admits a declared lookup-capable field such
as `Admins.Username`, not just the table's primary key. The result of `Lookup`
continues to implement `validation.Lookup[string]`, so keep an application's
canonicalizing lookup wrapper around it when one existed before. This helper
accepts the application's existing username rule explicitly:

<!-- orm-example: pilot-lookup -->
```go
type canonical struct {
	validation.Lookup[string]
}

func (c canonical) Exists(ctx context.Context, v string) (bool, error) {
	return c.Lookup.Exists(ctx, strings.ToLower(v))
}

func UniqueUsername[M any](executor database.Executor, source orm.Query[M], column orm.KeyColumn[M, string], username validation.Rule[string]) validation.Rule[string] {
	return validation.Bail(username, validation.Unique(canonical{validationdb.Lookup(executor, source, column)}))
}

func adminUsernameRule(tx *database.Tx, username validation.Rule[string]) validation.Rule[string] {
	return UniqueUsername(tx, Admins.Query().WithTrashed(), Admins.Username, username)
}
```

`WithTrashed()` includes soft-deleted values while retaining other source scopes.
Use it when the database unique constraint includes those rows. A lookup source
must remain unpaginated and have no ordering or eager loading. Validation is an
advisory read; it does not reserve the username or replace a database constraint.
For update exclusions and per-request scopes, see
[database validation](validation.md#advisory-database-rules).

## Preserve pagination and cursor contracts

Before:

<!-- orm-example: pagination-before -->
```go
func legacyPage(ctx context.Context, db database.Executor) error {
	_, err := old.QueryOrders().Paginate(ctx, db, query.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().CursorPaginate(ctx, db, query.CursorRequest[old.Order]{Size: 20})
	return err
}
```

After:

<!-- orm-example: pagination-after -->
```go
func page(ctx context.Context, db database.Executor) error {
	_, err := models.Orders.Query().Paginate(ctx, db, orm.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = orm.CursorPaginate(ctx, db, models.Orders.Query(), orm.CursorRequest[models.Order]{Size: 20})
	return err
}
```

Use ORM PageResult/SimplePage/CursorRequest/CursorPage names in service interfaces.
They alias the shared contracts behind legacy query pagination names. Preserve
existing public cursor identity, encoded bytes, stable ordering, nullable handling
and navigation assertions. Projected, joined and set reads use the rows result/cursor
adapters for their family; they cannot borrow a model write query to evade scope types.

## Register tables, identities and seeders

Before:

<!-- orm-example: application-before -->
```go
func legacyApplication(settings application.Settings) *application.Builder {
	return application.New(settings)
}
```

After:

<!-- orm-example: application-after -->
```go
func configuredApplication(settings application.Settings, pool foundation.Key[*database.DB]) *application.Builder {
	observers := foundation.Module{Name: "member.observers", OnRegister: func(r *foundation.Registrar) error {
		return orm.RegisterObserver(r, pool, orm.NewObserver[hooks.Member]("members"),
			func(foundation.Resolver) (func() orm.Hooks[hooks.Member], error) { return memberHooks, nil })
	}}
	return application.New(settings, application.WithTables(Users.Table, hooks.Members.Table)).Register(observers)
}
```

WithTables snapshots the explicit list and checks declarations before provider
resources start. Add every table; the framework does not scan packages at runtime.
Keep the configured database provider and its existing pool key in application
assembly. `orm.Check` and `rows.Check` support direct composition and row validation.

References reuse stored key codecs:

Before:

<!-- orm-example: identity-before -->
```go
func legacyIdentity(user old.User) (model.Identity, error) { return user.FoundryIdentity() }
```

After:

<!-- orm-example: identity-after -->
```go
func identity(user models.User) (model.Identity, error) {
	return orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user).Identity()
}
```

If auth, notifications or another contract requires `model.Identifiable`, preserve a
handwritten FoundryIdentity method delegating to the native identity declaration
before removing generated methods. Passing an ORM reference alone does not satisfy
that interface. Extension ownership also uses `orm.IdentityOf`; bind non-persisted
extension slots with SlotOf and the owning extension package's policies.

Seed definitions and caller-owned transactions stay unchanged:

Before:

<!-- orm-example: seed-before -->
```go
func legacySeeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := old.QueryUsers().Create(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test"))
		return err
	}}
}
```

After:

<!-- orm-example: seed-after -->
```go
func seeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := models.Users.Insert(ctx, tx, models.Users.Email.Set("ada@example.test"))
		return err
	}}
}
```

`factory.Define(table, build)` now builds assignment slices; preserve factory states,
relationship composition and after-create behavior. Seeders must call the ordinary
write pipeline rather than reproducing conventions or lifecycle callbacks.

## Use the current scaffolds

The command `foundry make model Widget --table widgets --dir ./models` now creates
a handwritten Widget, WidgetTable, Widgets declaration and WidgetTables helper.
The old result was an annotated model followed by generated query/draft helpers;
the new result follows the compiled role declaration above. Add fields and roles in
Go, then include the returned table list in application.WithTables. There is no
package-wide table discovery and no model generation step for new declarations.

Keep `foundry generate` for DTO/enum/config declarations. Model-with-extension
scaffolds still generate their metadata DTO codecs. `foundry make seeder --dir
seeders --name SeedUsers --id app.users` still creates an ordinary seed.Definition;
fill it with the compiled seeder body above. Migration scaffolds and schema SQL are
unchanged. Scaffolds refuse existing files and preserve handwritten edits. Reuse
[the scaffold compilation fixtures](../../internal/generate/scaffold_model_test.go)
and [migration/seeder guide](migrations-and-seeding.md#development-scaffolding).

## Move PostgreSQL type registration into connection setup

Main's accepted H1 change adds `postgres.Config.RegisterTypes` with signature
`func(context.Context, *postgres.TypeRegistry) error`. The callback runs once for
each physical connection before admission to the pool. Move custom native codec
registration and Go type mappings out of query rewriters and into this callback.

The registry provides setup-only `RegisterType`, `RegisterDefaultPgType` and
`LoadType` operations. Register the descriptor returned by `LoadType` explicitly,
loading and registering dependencies first as pgx requires. Type loading restores
the original search path after protected catalog discovery. The registry belongs
to the callback: do not retain it or use it after the callback returns. Registered
codecs and their application-owned state must remain immutable afterward.

A callback error, panic or `runtime.Goexit` prevents connection admission. A failed
registry operation also prevents admission, even if the callback ignores its error.
Native type registration inside a query rewriter, including re-registering the
same descriptor pointer, fails closed before the rewritten statement executes.
The framework returns a safe error naming `Config.RegisterTypes`, discards that
connection and does not automatically retry or replay the callback. Move the
registration into setup instead of retrying the rejected statement unchanged.

This contract is accepted on main but **is not integrated into this checkout**:
`database/postgres.Config` here still has no RegisterTypes field. Its compiling
example remains blocked until main's H1 reaches the branch. The migration does not
make ORM declarations automatically register arbitrary PostgreSQL types.

## Pilot recipes

These recipes address idioms identified during the pilot migration.

### Locked find by key

The legacy `ForUpdate().RequireFind(ctx, tx, key)` is replaced by `Where` with a typed key predicate and `Require`:

<!-- orm-example: pilot-recipe-locked-find -->
```go
func requireAdminForUpdate(ctx context.Context, tx *database.Tx, id int64) (Admin, error) {
	return Admins.Query().Where(Admins.Key().Eq(id)).ForUpdate().Require(ctx, tx)
}
```

Locks are available only on `LockedQuery` inside an active transaction (`*database.Tx`). Use `Require` to return `database.NotFound` on absence, or `First` when absence is handled with `value.Optional[M]`.

### Model reference and identity

When defining handwritten models in place of generated ones, attach the `FoundryReference` and `FoundryIdentity` method pair using `orm.IdentityOf` and `ReferenceOf`:

<!-- orm-example: pilot-recipe-model-identity -->
```go
var adminIdentity = orm.IdentityOf(Admins.Table, Admins.Key())

func (a Admin) FoundryReference() model.Reference[Admin, int64] {
	return adminIdentity.ReferenceOf(a)
}

func (a Admin) FoundryIdentity() (model.Identity, error) {
	return a.FoundryReference().Identity()
}
```

This declaration-level binding is required by `audit.RecordFor`, `audit.ModelHistory`, authentication providers (`auth.DefineProvider`), and authorization proofs.

### Datatable in isolated tests

Datatables execute `SET LOCAL search_path TO "<Schema>", pg_temp` on each request, overriding the connection's search path. When running isolated tests with `testkit/postgres.Isolate`, set `config.Schema = scope.Schema()`:

<!-- orm-example: pilot-recipe-datatable-test -->
```go
func isolatedDatatableConfig(scope *postgres.Scope) datatable.Config {
	config := datatable.DefaultConfig()
	config.Schema = scope.Schema()
	return config
}
```

Leaving `config.Schema` at its default (`"public"`) causes queries against an isolated schema to fail with PostgreSQL error `42P01` (`relation does not exist`).

### Generation is per package

The contract generator (`go tool foundry generate`) type-checks the complete package overlay. Any parse or type error in any file in that package—including unmigrated sibling files—fails the generator run. Convert a package's generated-model consumers atomically so all files in the package type-check together.

### Optional fields on insert

Construct an assignment slice and conditionally append optional values rather than chaining draft setters:

<!-- orm-example: pilot-recipe-optional-insert -->
```go
func insertAdmin(ctx context.Context, tx *database.Tx, tenantID int64, username string, email value.Optional[string]) (Admin, error) {
	assignments := []orm.Assignment[Admin]{
		Admins.TenantID.Set(tenantID),
		Admins.Username.Set(username),
	}
	if v, ok := email.Get(); ok {
		assignments = append(assignments, Admins.Email.Set(v))
	}
	return Admins.Insert(ctx, tx, assignments...)
}
```

Omitted fields are not passed to `Insert`, allowing database default values to apply. To explicitly write a SQL NULL on a nullable column, pass `column.SetNull()`.

## Preserve the remaining behavior

Numeric Increment/Decrement use positive finite magnitudes; AtMost bounds increments
and AtLeast bounds decrements. Keys, managed timestamps and input columns cannot gain
adjustment capability through adapters. Use PruneBatch or ForcePruneBatch with explicit
bounds and mass/per-model selection; the prune registry exposes ORMModel/ORMSoftModel.
Explain is read-only planning; ExplainAnalyze executes the statement. Typed explain
results and errors are redacted by default; explicit JSON/root access exposes plans.

Keep encrypted constructors and stored codecs. Queries cannot compare, sort or project
encrypted fields. Table.Raw validates complete hydration and bypasses scopes; executor
raw APIs remain explicit escape hatches. Preserve the project's no-raw policy.

The [breaking-change notes](../evidence/goway-breaking-changes.md) distinguish shipped
syntax changes, startup validation and the remaining publication prerequisites.

The before snippets above are historical, documentation-only examples. The after
snippets remain compiled by the migration fixture after model generation retirement.

## Pilot expressions and row declarations

Use `expr.Constant[S](codec, value)` for a bound row-phase value without borrowing
an unrelated column. It snapshots through the codec and binds a parameter; it does
not interpolate SQL. `expr.Row` adapts an existing row-phase expression, retaining
its scope and projection type. For nullable columns that type is nullable, even
when the column's comparison input is non-null. Selected/aggregate values cannot
be demoted with `Row`; keep column-based `expr.Param` for comparison-typed inputs.

<!-- orm-example: pilot2-row-constant -->
```go
func enabledAdmins() rows.Scalar[Admin, bool] {
	enabled := expr.When(expr.Value(Admins.Username).Eq("active"),
		expr.Constant[Admin](codec.Bool[bool](), true)).Else(expr.Constant[Admin](codec.Bool[bool](), false))
	return rows.SelectScalar(rows.From(Admins.Query()), enabled.Value())
}
func scopedUsername() {
	type adminScope struct{}
	source := rows.As[adminScope](Admins.Query(), "admin")
	parameter := expr.Row(expr.At(rows.Scope(source), Admins.Username)).Param("Ada")
	_ = parameter
}
```

`rows.BoolOf` and `orm.BoolOf` select Boolean codecs for built-in and named bools.
They reuse ordinary field/column carriers; mutated model fields still need Input
carriers. Framework enums already expose enum descriptors: use `rows.EnumOf` for
`countries.Status`, preserving its validation and enum capability restrictions.

<!-- orm-example: pilot2-boolean-enum -->
```go
type CountryFlag struct {
	Status  countries.Status
	Enabled bool
}
type CountryFlagColumns struct {
	rows.Row[CountryFlag]
	Status  rows.Field[CountryFlag, countries.Status]
	Enabled rows.Field[CountryFlag, bool]
}

var CountryFlags = rows.Define(func(b rows.Builder[CountryFlag], r *CountryFlag) CountryFlagColumns {
	return CountryFlagColumns{b.Row(), rows.EnumOf(b, &r.Status), rows.BoolOf(b, &r.Enabled)}
})
```

## Validate JSON property declarations at startup

`expr.JSONFieldOf` resolves and checks its selector at construction. Call `Validate`
to surface that stored error before executing queries. Go checks payload/result
types and field existence; startup checks also reject foreign pointers, ignored or
ambiguous JSON fields and opaque custom encodings. `orm.Check` does not register or
validate standalone JSON bindings. Validate every binding in your startup owner.

<!-- orm-example: pilot2-json-startup -->
```go
func validateJSONDeclarations() error {
	// JSONFieldOf already resolved the selector; surface its stored error at startup.
	// orm.Check validates tables, not standalone JSON field bindings.
	return Theme.Validate()
}
```

## Preserve an attachment slot's stored name

Replace a legacy slot tag such as `foundry:"name=public-media"` with the explicit
collection-name argument to `attachments.ManyOf` or `attachments.OneOf`. Native
slots have no persistence tags. `orm.SlotOf` binds the Go field; `extensions.BindSlot`
retains owner/key/value typing. Keep the collection name, extension owner identity
and `StorageModel` unchanged when renaming the Go field. The example binds Gallery
to the existing public-media collection; no stored-data migration is needed.

<!-- orm-example: pilot2-slot-name -->
```go
// The Go slot is Gallery; the persisted collection remains "public-media".
// Bind the field with orm.SlotOf in the model declaration, without a foundry tag.
var PublicMedia = attachments.ManyOf(ext.Owner, "public-media",
	attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}},
	extensions.BindSlot(ext.Articles.Key(), ext.Articles.Gallery))
```

See [extension slots](model-extension-slots.md), [JSON models](json-models.md) and
[projection declarations](model-projections.md) for the owning contracts.
