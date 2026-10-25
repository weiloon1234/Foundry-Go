# Migrate generated models to the ORM

Replace generated query, draft and projection helpers with handwritten declarations in
`database/orm`, `database/orm/rows` and `database/orm/expr`. Keep your model structs,
stored column names, key types, codecs, constraints and public response contracts.
This guide describes APIs present in the Go-way branch, not every illustrative API
in the original plan.

Migrate a shared model package before its callers. Register its tables, port its
queries and tests, then remove the old model directive and generated helpers at a
coordinated generation barrier. Keep DTO, enum, configuration and client generation.
Do not edit generated files or ownership manifests by hand. The branch temporarily
retains both APIs for migration and parity tests; that coexistence is not the target
application design.

## Use the compiled examples

Every Go block below is copied verbatim from the
[compiled example package](../../tests/fixtures/goway/migration/examples_test.go) or its
[SQL examples](../../tests/fixtures/goway/migration/sql_test.go).
[The guide test](../../tests/fixtures/goway/migration/guide_test.go) checks every block
against its source. The input examples also import `mutators`, `oldmutators` and their shared
`coremodels` input/storage types. The functions that accept an executor compile without opening
connections. Declaration validation and a read SQL/binding comparison run locally.
Run `go test ./migration` from `tests/fixtures/goway`.

Imports in the examples use `old` for the generated `oldmodels` fixture and `models`
for its native replacement. `oldhooks`/`hooks` and `oldreads`/`reads` are corresponding
lifecycle and tenancy fixtures. `reports` is an existing fixture that currently
contains both generated projection helpers and native row declarations over the same
DTO. `Users` without a package qualifier is the standalone declaration below.
Copy the corresponding imports and concrete model types from the linked source.

## Declare models and lifecycle roles

Old declarations use `//foundry:model table=...` and generated `XFields()`/`XDraft`.
For example, [the legacy lifecycle model](../../tests/fixtures/goway/oldmodels/hooks/models.go)
uses `hooks=LocalHooks`. The ordinary struct and its domain methods survive migration.
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

`NumberKey` and `TextKey` preserve key roles. They do not make a primary key an
ordinary writable numeric/text field or grant numeric adjustment operations.
`MapInsert` rejects input fields and managed UpdatedAt; CreatedAt is eligible.
`MapUpdate` also rejects primary keys. Application wrappers cannot manufacture these
roles by embedding or converting a column.

Keep `Mutate<Field>` and `Access<Field>` methods as ordinary Go methods. Pass `nil`
as the input constructor's transform to discover a named mutator at startup, as above.
Use the constructor's typed transform for an explicit distinct-input binding;
`orm.Mutate(fn)` is the same-type option on an Input carrier. Do not configure both
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

`LoadFrom`/`LoadMissingFrom` are an accepted Lane A addition but **are not present in
this checkout**. Their accepted signature takes `(ctx, executor, query, parents)`;
the query supplies its `With` tree and relation limits. Do not replace that behavior
with plain `Load` if the caller needs query-specific limits. This section and its
compiled example require the lead's integration before Stage 10 publication.

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
	f, r := reports.MemberModels, reports.MemberRows
	return rows.Select(rows.From(f.Query()), r,
		rows.Map(r.ID, expr.Value(f.ID)), rows.Map(r.Name, expr.Value(f.Name)),
		rows.Map(r.Nickname, expr.Value(f.Nickname)), rows.Map(r.State, expr.Value(f.State)),
		rows.Map(r.Balance, expr.Value(f.Balance)))
}
```

Declare each concrete row with `rows.Define`, embed `rows.Row[R]` from `b.Row()`, and
bind fields with rows constructors. `rows.Map` matches the exact projected type;
`MapSelected` accepts a selected expression. The existing
[row declaration fixture](../../tests/fixtures/goway/oldreporting/rows.go) shows the
complete MemberRows schema used above. All fields must be mapped exactly once.

A SQL row is not implicitly an API response. Replace `//foundry:projection dto=true`
with a handwritten rows declaration plus retained `//foundry:dto` generation at the
serial barrier. Preserve the DTO name, JSON tags, validation rules and descriptors;
DTO/client generation still runs. Plain internal projections need no DTO annotation.
Do not expose persistence models as response DTOs or change contract bytes to make
migration easier.

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
func tableSource(spec datatable.Spec[reports.Member, reports.MemberRow, reports.Authority], tenant func(context.Context, reports.Authority) (int64, error)) datatable.Spec[reports.Member, reports.MemberRow, reports.Authority] {
	// Keep the existing Spec authorization; tenant resolves the trusted actor scope.
	spec.Source = func(ctx context.Context, actor reports.Authority) (rows.Query[reports.Member, reports.MemberRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[reports.Member, reports.MemberRow]{}, err
		}
		return report().Where(expr.Value(reports.MemberModels.TenantID).Eq(id)), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Value(reports.MemberModels.ID).Asc()}
	return spec
}
```

Pass an authorized, tenant-constrained native source and retain the existing Spec
ID, DTO descriptor, columns, labels, filters and export capabilities. The complete
[datatable fixture](../../tests/fixtures/goway/oldreporting/members.go) uses
`rows.Select`, `expr.Select`, `expr.TextRow`, nullable filters and typed related filters.
The fixture's historical package name does not mean its datatable Spec still uses
the old query engine. Do not bypass actor resolution or reconciliation to adapt a
source signature. Manifests, OpenAPI, TypeScript and filter/response bytes stay fixed.

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
