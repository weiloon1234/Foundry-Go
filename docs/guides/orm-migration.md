# Migrate generated models to the ORM

Replace retired query, draft and projection helpers with typed ORM APIs in
`database/orm`, `database/orm/rows` and `database/orm/expr`. Keep your model structs,
stored column names, key types, codecs, constraints and public response contracts.
This guide describes APIs present in the Go-way branch, not every illustrative API
in the original plan.

Migrate a shared model package before its callers. Declare its tables with the
`//foundry:model` directive ([model declarations](model-declarations.md)), port its
queries and tests, then remove the old generated query, draft and projection helpers
at a coordinated generation barrier. Keep DTO, enum, configuration and client
generation. Do not edit generated files or ownership manifests by hand. Projection
generation and the former query runtime are retired; a model's table type, binding
and identity are generated from its struct.

## Unified ORM column handles and operators

Every declared model column type (`orm.Column`, `orm.Key`, `orm.Primary`, `orm.Number`, `orm.Text`, `orm.Time`, `orm.Enum`, `orm.JSON`, `orm.Interval`, `orm.CreatedAt`, `orm.UpdatedAt`, `orm.SoftDelete`, `orm.Nullable*`, `orm.Input*`, `orm.Encrypted`, `orm.NullableEncrypted`) is now a type alias over the 6-parameter, methodless carrier `orm.ColumnHandle[M, V, I, P, K, R]`. Pass column handles directly to unified operators, expressions, row mappings, relations, conflict policies, audit/change helpers, validation lookups, model bindings, and datatable constructors without `*Expr` wrappers.

| Family (removed) | Unified replacement | Notes |
| --- | --- | --- |
| `orm.<Kind>Eq`, `<Kind>Ne`, `<Kind>In`, `<Kind>NotIn` | `orm.Eq`, `orm.Ne`, `orm.In`, `orm.NotIn` | Requires `orm.ColumnKind` (rejects encrypted). |
| `orm.<Kind>Gt`, `<Kind>Gte`, `<Kind>Lt`, `<Kind>Lte`, `<Kind>Between` | `orm.Gt`, `orm.Gte`, `orm.Lt`, `orm.Lte`, `orm.Between` | Requires `orm.OrderedKind` (Number, NumberKey, Text, Time, Interval). |
| `orm.<Kind>Like`, `<Kind>ILike`, `<Kind>Contains`, `<Kind>IContains`, `<Kind>StartsWith`, `<Kind>IStartsWith`, `<Kind>EndsWith`, `<Kind>IEndsWith` | `orm.Like`, `orm.ILike`, `orm.Contains`, `orm.IContains`, `orm.StartsWith`, `orm.IStartsWith`, `orm.EndsWith`, `orm.IEndsWith` | Requires `orm.TextColumnKind` (rejects string-backed enums and keys). |
| `orm.<Kind>JSONContains`, `<Kind>JSONContainedBy`, `<Kind>JSONIsJSONNull` | `orm.ContainsDocument`, `orm.ContainedByDocument`, `orm.IsJSONNull` | Requires `orm.JSONColumnKind`. |
| `orm.<Kind>IsNull`, `<Kind>IsNotNull`, `<Kind>SetNull` | `orm.IsNull`, `orm.IsNotNull`, `orm.SetNull` | Requires `orm.NullableColumnKind` (includes `NullableEncrypted`). |
| `orm.<Kind>Set` | `orm.Set`, `orm.SetIfPresent` | Requires `orm.AnyColumnKind` (includes `Encrypted`); `Set` takes input `I`. |
| `orm.<Kind>Asc`, `<Kind>Desc` | `orm.Asc`, `orm.Desc` | Requires `orm.ColumnKind`. |
| `orm.NumberIncrement`, `NullableNumberIncrement`, `NumberDecrement`, `NullableNumberDecrement` | `orm.Increment`, `orm.Decrement` | Requires `orm.NumberColumnKind` and `orm.AdjustableRole`. |
| `orm.<Kind>Expr(col)` (all 33 wrappers) and `*Adapter` / `KeyColumn` / `ExpressionColumn` / `InsertTarget` / `UpdateTarget` | Pass `col` directly | Expressions, row mappings, conflict keys, lookups, and model bindings accept `ColumnHandle` directly. |
| `orm.ManyToManyOf`, `HasOneThroughOf`, `HasManyThroughOf`, `MorphOneOf`, `MorphManyOf`, `MorphToOf`, `MorphToManyOf` (flat key args) | Wrap each key pair in `orm.JoinKeys(a, b)` | `BelongsToOf`, `HasOneOf`, `HasManyOf` keep direct `(slot, local, foreign)`. |
| `orm.AuditEncryptedField`, `AuditNullableEncryptedField`, `AuditReadEncrypted`, `AuditReadNullableEncrypted`, `EncryptedChangeOf`, `NullableEncryptedChangeOf` | `orm.AuditField`, `orm.AuditRead`, `orm.AuditReadNullable`, `orm.ChangeOf`, `orm.NullableChangeOf` | `AuditRead` and `ChangeOf` now enforce non-null columns (`orm.RequiredAnyColumnKind`) at compile time. |
| Direct single-column `datatable.DefineColumn` wiring | `datatable.Text`, `Number`, `Time`, `Enum`, `Bool`, `NullableText`, `NullableNumber`, `NullableTime`, `NullableEnum` | Enable sort, filter, export, and (for text) search by default; opt out via `datatable.NoSort`, `NoFilter`, `NoSearch`, `NoExport`. `datatable.DefineColumn` is removed; joined, computed, or custom-codec columns use `datatable.Column` (see [datatable columns and sources](#datatable-columns-and-sources)). |

## Datatable columns and sources

A datatable is now its columns plus its base source. Each column selects its own
row field, so the handwritten `…RowColumns` struct, the `rows.Define` schema and
the `rows.Select(…, rows.Map(…))` list of a table are removed. `datatable.DefineColumn`
is removed with no alias; `datatable.Column` is its general form. `rows.Define`,
`rows.Select` and `rows.Map` remain for SQL results that are not tables (see
[declared query projections](model-projections.md#recipe-declare-rows)). Convert
every table before updating the framework pin. SQL text, stored data, wire formats
and generated clients do not change.

| From | To |
| --- | --- |
| `//foundry:dto` on a table's row struct | `//foundry:dto role=row`, then `foundry generate`. The generator emits `XRowColumns()` (typed `validation.Binding` values) in place of `XRowValidationFields()` for that DTO. |
| `foundry make dto X` for a table row | `foundry make dto X --role row` |
| Handwritten `…RowColumns` struct and `var …Rows = rows.Define(…)` | Delete both |
| `w := XRowValidationFields()` | `w := XRowColumns()` (the variable and every `datatable.Text(w.Name, …)` call stay as they are) |
| `validation.FieldName(w.X)` in `DefaultSort` | `validation.BindingName(w.X)` |
| `datatable.Text`, `Number`, `Time`, `Enum`, `Bool`, `NullableText`, `NullableNumber`, `NullableTime`, `NullableEnum` | Unchanged at the call site; the first argument is now a binding |
| `datatable.DefineColumn[S](w.X, label, options...)` | `datatable.Column(w.X, label, selected, options...)`, where `selected` is the `expr.Selected` the column reads (previously the `rows.Map` expression) |
| A row field that the query selects but no column shows (a key, a foreign key) | `datatable.Hidden(w.ID, Admins.Key())` for a model column, or `datatable.HiddenSelected(w.ID, selected)` for a joined or computed expression |
| A column whose expression depends on the request (locale label, current time) | `datatable.NewDynamicSlot`, `datatable.DynamicColumn` and `Spec.Dynamic` with `datatable.BindDynamic` |
| A custom scan codec in `rows.Define` | `datatable.ScanWith[S](orm.Codec(…))` as an option of `Column`, `DynamicColumn` or `HiddenSelected` |
| `Spec.Source` returning `rows.Select(rows.From(query), XRows, rows.Map(…))` | `Spec.Source` returning `rows.From(query)` or a join |
| `Spec.Source` returning `rows.Select(…).Where(…)`, `rows.GroupBy(…)`, `rows.Having(…)` or `rows.Distinct(…)` | `rows.WhereSource(source, …)`, `rows.GroupSource(source, …)`, `rows.HavingSource(source, …)` or `rows.DistinctSource(source)` around the base source |
| `Spec.Source` of type `func(ctx, A) (rows.Query[S, R], error)` | `func(ctx, A) (rows.SelectionSource[S], error)` |
| A test that calls `spec.Source(ctx, authority)` and compiles the returned query | `datatable.Compile(ctx, table, config, subject, request)`, which returns the row and count statements of a request through the real request path, without a database; see [pin a table's SQL in a test](datatable.md#recipe-compile-table-sql) |

A binding's name must equal the JSON property of the struct field it addresses;
`datatable.Define` rejects a mismatch. Before:

<!-- orm-example: datatable-columns-before -->
```go
//foundry:dto
type AdminRow struct {
	ID       model.ID[models.Admin] `json:"id"`
	Username string                 `json:"username"`
}

type adminRowColumns struct {
	rows.Row[AdminRow]
	ID       rows.Field[AdminRow, model.ID[models.Admin]]
	Username rows.Field[AdminRow, string]
}

var adminRows = rows.Define(func(b rows.Builder[AdminRow], r *AdminRow) adminRowColumns {
	return adminRowColumns{Row: b.Row(), ID: rows.IDOf(b, &r.ID), Username: rows.TextOf(b, &r.Username)}
})

// In Spec:
//   Columns: datatable.Text(w.Username, "admins.fields.username", a.Username), …
//   Source: rows.Select(rows.From(a.Query()), adminRows,
//       rows.Map(adminRows.ID, expr.Value(a.Key())), rows.Map(adminRows.Username, expr.Value(a.Username)))
```

After: the [declarations](#replace-the-projection-declaration-and-source-callback)
below show the converted `AdminRow`, its `datatable.Hidden` and `datatable.Text`
columns and a `Source` that returns `rows.From(…)`, compiled with the framework.

## Removed API spellings

This documentation-only migration table covers the retirement denial ledger and
the former shared contracts. Use the native column, scope and read families in
the compiled examples below; there is no compatibility import or alias.

<!-- orm-example: retired-surface-before -->
```text
Removed spelling | Native replacement
--- | ---
attachments.AddFiles | attachments.AddFilesTo
attachments.Collection.Matching | attachments.Matching
attachments.DefineMany | attachments.ManyOf with extensions.BindSlot
attachments.DefineOne | attachments.OneOf with extensions.BindSlot
attachments.ManySlot | attachments.ManyHandle
attachments.ManySlot.Add | attachments.ManyHandle.Add
attachments.ManySlot.AddFile | attachments.ManyHandle.AddFile
attachments.ManySlot.AddIn | attachments.ManyHandle.AddIn
attachments.ManySlot.Format | attachments.ManyHandle.Format
attachments.ManySlot.From | attachments.ManyHandle.From
attachments.ManySlot.PublicURLs | attachments.ManyHandle.PublicURLs
attachments.ManySlot.Reorder | attachments.ManyHandle.Reorder
attachments.OneSlot | attachments.OneHandle
attachments.OneSlot.Format | attachments.OneHandle.Format
attachments.OneSlot.From | attachments.OneHandle.From
attachments.OneSlot.PublicURL | attachments.OneHandle.PublicURL
orm.Extension | handle.From(manager).Relation(slot)
orm.LegacyIdentityScope | orm.ModelIdentity.Scope on a validated identity
orm.Predicate.Not | orm.Not(predicate)
orm.Load | orm.LoadFrom(ctx, executor, query.With(relations...), parents)
orm.LoadMissing | orm.LoadMissingFrom(ctx, executor, query.With(relations...), parents)
orm.MorphName | model.MorphName
orm.Morphable | model.Morphable
orm.Mutate | model Mutate<Field> method and typed orm.Input* declaration
prune.Model | prune.ORMModel or prune.ORMSoftModel
extensions.DefineOwner | extensions.DefineORMOwner with the generated identity and OwnerOptions
extensions.DefineOwnerWith | extensions.DefineORMOwner with explicit OwnerOptions
extensions.Owner.QueryScope | extensions.ORMIdentity(owner).Scope(keys...)
modelbinding.ByField | modelbinding.ByColumn
modelbinding.Through | modelbinding.ThroughRelation
modelbinding.ThroughSelected | modelbinding.ThroughSelectedRelation
metadata.DefineValue | metadata.ValueOf with extensions.BindSlot
metadata.Key.Matching | metadata.Matching
metadata.ValueSlot | metadata.ValueHandle
metadata.ValueSlot.Describe | metadata.ValueHandle.Describe
metadata.ValueSlot.Forget | metadata.ValueHandle.Forget
metadata.ValueSlot.ForgetIn | metadata.ValueHandle.ForgetIn
metadata.ValueSlot.Format | metadata.ValueHandle.Format
metadata.ValueSlot.From | metadata.ValueHandle.From
metadata.ValueSlot.Key | metadata.Define for a standalone key; a bound handle retains its descriptor privately
metadata.ValueSlot.Name | metadata.ValueHandle.Name
metadata.ValueSlot.Registration | metadata.ValueHandle.Registration
metadata.ValueSlot.Save | metadata.ValueHandle.Save
metadata.ValueSlot.SaveIn | metadata.ValueHandle.SaveIn
metadata.ValueSlot.Validate | metadata.ValueHandle.Validate
factory.Model | factory.Define with a typed table and assignments
factory.New | factory.Define
translations.DefineText | translations.TextOf with extensions.BindSlot
translations.Field.Matching | translations.Matching
translations.TextSlot | translations.TextHandle
translations.TextSlot.Clear | translations.TextHandle.Clear
translations.TextSlot.ClearIn | translations.TextHandle.ClearIn
translations.TextSlot.Describe | translations.TextHandle.Describe
translations.TextSlot.Field | translations.Define for a standalone field; a bound handle retains its descriptor privately
translations.TextSlot.Forget | translations.TextHandle.Forget
translations.TextSlot.ForgetIn | translations.TextHandle.ForgetIn
translations.TextSlot.Format | translations.TextHandle.Format
translations.TextSlot.From | translations.TextHandle.From
translations.TextSlot.MergeRule | translations.TextHandle.MergeRule
translations.TextSlot.Name | translations.TextHandle.Name
translations.TextSlot.Options | translations.TextHandle.Options
translations.TextSlot.Registration | translations.TextHandle.Registration
translations.TextSlot.Rule | translations.TextHandle.Rule
translations.TextSlot.Save | translations.TextHandle.Save
translations.TextSlot.SaveIn | translations.TextHandle.SaveIn
translations.TextSlot.Sync | translations.TextHandle.Sync
translations.TextSlot.SyncIn | translations.TextHandle.SyncIn
translations.TextSlot.Validate | translations.TextHandle.Validate
rows.Count | rows.Read(query).Count(ctx, executor)
rows.Compile | rows.CompileRead
rows.Preflight | rows.PreflightRead
rows.Each | rows.EachRead
rows.Paginate | rows.Read(query).Paginate
rows.SimplePaginate | rows.Read(query).SimplePaginate
rows.Query.OrderBy | rows.OrderBy(query, orders...)
rows.ExplainLocked | rows.ExplainTransaction
rows.ExplainAnalyzeLocked | rows.ExplainAnalyzeTransaction
orm.DoesntHave | orm.Not(orm.Has(relation, predicates...))
expr.JSONOrderedScalar | expr.OrderNullableRow(expr.JSONScalar(path, codec))
expr.NullFor | expr.ParamOf(expr.NullableRow(v), value.Null[T]())
extensions.Scope | extensions.ORMIdentity(owner).Scope
extensions.Owner.ModelName | extensions.ORMIdentity(owner).ModelName
extensions.Owner.Reference | extensions.ORMIdentity(owner).Reference
extensions.Owner.Active | extensions.Owner.ActiveSubjects (discard the ordered-key result when unused)
rows.ScalarQuery | expr.AsSelected(rows.ScalarRowQuery(...))
rows.ScalarNullableQuery | expr.AsSelected(rows.ScalarNullableRowQuery(...))
rows.CorrelatedScalarQuery | expr.AsSelected(rows.CorrelatedScalarRowQuery(...))
rows.CorrelatedScalarNullableQuery | expr.AsSelected(rows.CorrelatedScalarNullableRowQuery(...))
rows.TransactionScalarQuery | expr.AsSelected(rows.TransactionScalarRowQuery(...))
rows.TransactionScalarNullableQuery | expr.AsSelected(rows.TransactionScalarNullableRowQuery(...))
rows.TransactionCorrelatedScalarQuery | expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(...))
rows.TransactionCorrelatedScalarNullableQuery | expr.AsSelected(rows.TransactionCorrelatedScalarNullableRowQuery(...))
rows.Query.All | rows.Read(query).All
rows.RecordRead.All | rows.ReadRecord(record).All
rows.SetQuery.All | rows.ReadSet(set).All
rows.ValueSetQuery.All | rows.ReadValues(values).All
rows.ValueCursorQuery.Key | expr.Group(cursor.Value())
expr.Select | expr.AsSelected(row); expr.AsSelected(aggregate)
expr.JSONKind | database.JSONKind
expr.JSONObject | database.JSONObject
expr.JSONArray | database.JSONArray
expr.JSONString | database.JSONString
expr.JSONNumber | database.JSONNumber
expr.JSONBoolean | database.JSONBoolean
expr.JSONNull | database.JSONNull
expr.JSONPath.Kind | expr.JSONTypeNullable(path.JSON())
expr.JSONPath.Length | expr.JSONLengthNullable(path.JSON())
expr.JSONPath.Exists | path.JSON().IsNotNull()
expr.JSONPath.IsMissing | path.JSON().IsNull()
expr.JSONPath.IsJSONNull | expr.JSONTypeNullable(path.JSON()) compared with database.JSONNull
expr.JSONPath.Contains | expr.JSONContainsNullable(path.JSON(), operand)
datatable.Config.DownloadTimeout | datatable.Manager.DownloadTimeout (including a nil manager)
model.NewID | model.NewIDAt
jobs.LegacyEnvelope | jobs.CurrentEnvelopeVersion; format 3 only (convert retained formats 1/2 before activation)
database/query (all builders and execution) | database/orm declarations and reads; orm/expr expressions; orm/rows projections, sets and CTEs
query.PageRequest | orm.PageRequest
query.Page | orm.PageResult
query.SimplePage | orm.SimplePage
query.CursorRequest | orm.CursorRequest
query.CursorPage | orm.CursorPage
query.Cursor | orm.Cursor
query.ParseCursor | orm.ParseCursor
query.CursorInputError | orm.CursorInputError
query.Plan | orm.Plan
query.PlanNode | orm.PlanNode
query.Statement | orm.Statement
query.WriteError | orm.WriteError
query.RelationLimits | orm.RelationLimits
query.DefaultRelationLimits | orm.DefaultRelationLimits
query.MaxExpressionDepth | orm.MaxExpressionDepth
query.MaxExpressionNodes | orm.MaxExpressionNodes
query.MaxParameters | orm.MaxParameters
query.MaxMembershipValues | orm.MaxMembershipValues
query.MaxScalarSQLBytes | orm.MaxScalarSQLBytes
query.MaxPageSize | orm.MaxPageSize
query.MaxCursorBytes | orm.MaxCursorBytes
query.MaxCursorFields | orm.MaxCursorFields
query.MaxInsertRows | orm.MaxInsertRows
query.MaxPerModelWriteRows | orm.MaxPerModelWriteRows
query.MaxRelationWriteRows | orm.MaxRelationWriteRows
query.MaxIdentityBatch | orm.MaxIdentityBatch
query.MaxChunkSize | orm.MaxChunkSize
query.DefaultChunkSize | orm.DefaultChunkSize
query.JSONKind | database.JSONKind
query.JSONNull | database.JSONNull
query.JSONBoolean | database.JSONBoolean
query.JSONNumber | database.JSONNumber
query.JSONString | database.JSONString
query.JSONArray | database.JSONArray
query.JSONObject | database.JSONObject
query.MorphName | model.MorphName
query.Morphable | model.Morphable
query.DateUnit | expr.DateUnit
query.TimestampUnit | expr.TimestampUnit
query.TimeZone | expr.TimeZone
query.LocalResolution | expr.LocalResolution
query.GeographicPoint | expr.GeographicPoint
query.MaxPlanBytes | internal policy; no application-level replacement
query.MaxPlanNodes | internal policy; no application-level replacement
query.MaxPlanDepth | internal policy; no application-level replacement
query.MaxLifecycleDepth | internal policy; no application-level replacement
query.ModelField / RecordField | rows.Field[R,V] and typed projection columns
query.ConflictValueField / ModelValueField | orm conflict expressions and typed assignments
query.ModelScope / RecordScope | rows.As / rows.Scope
query.NullableModelScope / NullableRecordScope | outer-join nullable row scopes
FoundryExplain / FoundryObservers bridges | native Explain terminals / registered ORM observers; no application capability token
```

`orm.Find`, `orm.RequireKey`, `orm.Update` and `orm.Delete` are the keyed
model operations, including hooks and duplicate-row diagnostics. Scoped
`orm.Update` and `orm.Delete` perform keyed `writeOne` operations within the
query's visibility, retaining lifecycle and cardinality checks. Row-value
carriers and aggregates use `expr.AsSelected`; selected read families share
`CompileRead`, `PreflightRead`, `EachRead` and `Result` terminals. Missing JSON paths,
SQL NULL and JSON null remain distinct. Loading existing parents uses a query so
its relation depth/row budgets and already-loaded-slot policy survive.

The cursor descriptor now publishes the native ORM type ID. Regenerate clients
and contract manifests together; token bytes and model identity are unchanged.
The version-1 job envelope has a versioned name; queued bytes need no migration.

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
`Users` without a package qualifier belongs to the linked migration fixture. Copy the
current imports and concrete model types from the linked source.

## Declare models and lifecycle roles

Models use
`//foundry:model table=users var=Users key=ID` on the struct and `foundry` field
tags. Do not write a table type, binding or identity block yourself.

The [complete plain model](model-declarations.md#recipe-declare-table) is rendered
from the independent consumer fixture.

See [model declarations](model-declarations.md) for nullable fields, enums,
decimals, foreign keys, timestamps, mutator input types and slot ownership.
Managed timestamps require `role=created-at` and `role=updated-at` together.
A lone ordinary `CreatedAt` uses `role=time` with conventions enabled; keep any
existing `timestamps=none` choice for an application-managed model.
`role=time` cannot disable an active timestamp pair or soft-delete convention.

An exposed key uses `member=ID`; add `key-role=primary` only for insert-select.
A mutated key uses `input=Type,member=ID,key-role=input`. Mutated fields and managed
update timestamps cannot be SQL assignment targets. Primary keys never support
source updates. These restrictions remain in the generated handle types.

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
	return orm.Set(mutators.Members.Email, input)
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
	return orm.Set(mutators.KeyRecords.ID, mutators.FreshID{Value: id})
}
```

The [input-key declaration](../../tests/fixtures/goway/models/mutators/key.go) uses
`member=ID,key-role=input,input=FreshID` on its primary field. The stored ID still drives Find.
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
	_, err := orm.All(ctx, db, models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100)))
	if err != nil {
		return err
	}
	_, err = orm.Find(ctx, db, models.Orders.Table, id)
	if err != nil {
		return err
	}
	_, err = orm.Require(ctx, db, models.Orders.Query().Where(orm.Eq(models.Orders.ID, id)))
	return err
}
```

`Where`, `OrderBy`, `Limit` and `Offset` remain methods on typed model queries.
Read terminals are package functions: `orm.First`, `orm.All`, `orm.Count`,
`orm.Exists` and `orm.Require`. Locked model queries use `orm.FirstLocked`,
`orm.AllLocked` and `orm.RequireLocked`; rows result adapters retain `Result.RequireFirst`.
`orm.Lock(query)` and the other lock selections retain the `*database.Tx`
requirement at the locked terminals. Scopes and soft-delete visibility stay attached.

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
	_, err := orm.Insert(ctx, tx, models.Users.Table, orm.Set(models.Users.Email, "ada@example.test"))
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
	_, err := orm.Update(ctx, tx, hooks.Members.Table.Query(), hooks.Members.Table.Key(), id, orm.Set(hooks.Members.Name, "Ada"), orm.SetNull(hooks.Members.Note))
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
	_, err := orm.UpdateAll(ctx, tx, orm.WithoutModelHooks(hooks.Members.Query()), orm.Set(hooks.Members.Name, "guest"))
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

Keep `relation.One`, `relation.Many`, `relation.Through` and other slots on the
model. Regenerate their typed slot handles. Declare relation
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
	_, err := orm.All(ctx, db, models.Orders.Query().With(OrderBuyer))
	if err != nil {
		return err
	}
	_, err = orm.LoadFrom(ctx, db, models.Orders.Query().With(OrderBuyer), existing)
	return err
}
```

`HasManyOf`, `HasOneOf`, many-to-many/pivot, through, morph and one-of-many declarations
follow the same typed slot/key pattern. Existing relationship filters and aggregates
use the declared relation (`orm.Has`, `orm.Not(orm.Has(...))`, and typed aggregates).
`With` composes the eager tree; `orm.LoadFrom` returns copies of existing parents and
`orm.LoadMissingFrom` preserves already loaded slots. Relations keep target/pivot scopes,
soft-delete visibility, bounded batches and all-or-error attachment.

`LoadFrom`/`LoadMissingFrom` take `(ctx, executor, query, parents)`; the query
supplies its `With` tree and relation limits. The supplied parent slice
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
		if !orm.WriteIsSet(draft, func() func(arg0 string) orm.Assignment[hooks.Member] {
			receiver := hooks.Members.Name
			return func(arg0 string) orm.Assignment[hooks.Member] { return orm.Set(receiver, arg0) }
		}()) {
			return orm.WriteSet(draft, orm.Set(hooks.Members.Name, "guest"))
		}
		return nil
	}}
}
```

Attach the package-local per-write factory with `hooks=memberHooks` on the directive. Keep all
old stages and their ordering: Saving precedes Creating/Updating; Created/Updated
precedes Saved. Deleting/Deleted, Restoring/Restored, ForceDeleting/ForceDeleted and
AfterCommit retain their operation-specific behavior. Local callbacks run before
observer callbacks. Hooks share the effective write; after callbacks receive stored
changes, not speculative input.

Pass a typed setter callback, such as `func(v string) orm.Assignment[User] { return orm.Set(Users.Email, v) }`,
to `WriteGet`, `WriteNullableGet`, or `WriteIsSet`; assign
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
	return orm.WriteUnset(draft, func() func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
		receiver := hooks.Members.Note
		return func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
			return orm.Set(receiver, arg0)
		}
	}())
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

Name a package-local factory with `retrieval=memberRetrieval` and register retrieval observers with
`NewRetrievalObserver`/`RegisterRetrievalObserver`. The local factory precedes observer
factories for each nonempty complete batch. Each complete model receives Retrieved
after decoding and row closure. Streaming uses closed batches when callbacks exist.
DTO/scalar reads and internal write hydration skip retrieval. Complete-model
`orm.Raw` dispatches retrieval but bypasses global scopes; its SQL must enforce
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
		return orm.Eq(t.TenantID, id), err
	})
}
```

Attach a lazy immutable source with `scopes=scopeFactory` on the directive.
Its package-local signature is `func(ModelTable) []orm.GlobalScope[Model]`. The source receives the completed table;
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
	_, err := orm.Upsert(ctx, tx, f.Query(), policy, orm.Set(f.Email, "ada@example.test"), orm.Set(f.Age, 30))
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
	_, err := orm.FirstOrCreate(ctx, tx, models.Users.Query().Where(orm.Eq(models.Users.Email, "ada@example.test")))
	return err
}
```

`orm.UpdateOrCreate` takes separate creation assignments and an update callback
returning assignments. `orm.CreateOrFirst` runs insertion in a savepoint, and only an own-table unique violation
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
		rows.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt))).Values(orm.Set(f.Name, "copy"))
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
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).Values(orm.Set(f.Name, "updated")).Exec(ctx, tx)
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
	_, err := rows.Read(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents)).All(ctx, db)
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

A SQL row is not implicitly an API response. Replace the retired projection directive with DTO output
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
	q := rows.GroupByScalar(rows.SelectScalar(rows.From(models.Orders.Query()), expr.AsSelected(count)),
		expr.Group(expr.Value(models.Orders.BuyerID)))
	_, err := rows.ReadScalar(rows.HavingScalar(q, expr.GtValue(count, 1))).All(ctx, db)
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
		OrderBy(expr.Desc(expr.At(s, models.Orders.TotalCents)))
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
	cte := rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100))))
	a := rows.AsRecord[orderSide](cte, "o")
	_, err := rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a))).All(ctx, db)
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
	a := models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100))
	_, err := rows.ReadSet(rows.Union(rows.ModelRecord(a), rows.ModelRecord(models.Orders.Query()))).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = rows.Read(rows.Distinct(rows.FromModel(models.Orders.Query()))).All(ctx, db)
	return err
}
```

`rows.FromModel` is the complete-model read adapter for ordinary row clauses.
Set operations preserve complete record types. Use the corresponding value/result
adapters for scalar or projected rows; read-only composition does not acquire writes.
`rows.EachRead` streams bounded complete-record batches and decoded
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
the unselected base `rows.SelectionSource[S]`; each column declares what it selects,
and columns and filters use phase-appropriate expr values:

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
	spec.Source = func(ctx context.Context, actor A) (rows.SelectionSource[reports.Member], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return nil, err
		}
		return rows.From(reports.Members.Query().Where(orm.Eq(reports.Members.TenantID, id))), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Asc(expr.Value(reports.Members.ID))}
	return spec
}
```

Pass an authorized, tenant-constrained native source and retain the existing Spec
ID, DTO descriptor, columns, labels, filters and export capabilities. Column
`SortBy` accepts `expr.AsSelected(expr.Row(expr.Value(Members.Name)))` or `expr.AsSelected` on a comparison adapter
for enums (`expr.AsSelected(expr.CompareRow(expr.Value(Members.State)))`), while `Stable`
takes the short `expr.Asc(expr.Value(MemberModels.ID))` spelling. The complete
[datatable fixture](../../tests/fixtures/consumer/reporting/members.go) uses
`datatable.Column`, `expr.AsSelected(expr.Row(row))`, `expr.TextRow`, nullable filters and typed related filters.
Do not bypass actor resolution or reconciliation to adapt a
source signature. Manifests, OpenAPI, TypeScript and filter/response bytes stay fixed.

### Port the application boundaries from the pilot

The [datatable guide](datatable.md) describes request operators, authorization and
exports. Use this table when replacing a generated field namespace `f` with an
ORM declaration. `RowEquality` and `RowNullable` are interfaces, not constructors;
`datatable.Where` and `NullableWhere` still construct the filters.

| Before | After |
| --- | --- |
| Generated field sorting | `datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(f.Username))))`, with `f` now the ORM declaration. The selected value must match the DTO field type, including nullability. |
| `datatable.Where(text, f.Username)` | `datatable.Where(text, expr.TextFilter(expr.TextRow(f.Username)))`. |
| `datatable.NullableWhere(text, f.Email)` | `datatable.NullableWhere(text, expr.NullableTextFilter(expr.TextNullableRow(f.Email)))`. |
| Equality-only or enum `datatable.Where(codec, f.X)` | `datatable.Enum(wire.State, label, f.State, codec)` for direct columns, or `datatable.Where(codec, expr.CompareFilter(expr.CompareColumn(f.State)))` with `datatable.Column`. Computed row values use `expr.CompareFilter(row)` directly. Nullable columns use `datatable.NullableEnum` or `NullableCompareFilter` and `NullableWhere` with a present-value codec. |
| Ordered numeric `datatable.Where(codec, f.X)` | `datatable.Number(wire.Amount, label, f.Amount, codec)` for direct columns, or `datatable.Where(codec, expr.OrderFilter(expr.OrderedColumn(f.Amount)))`; nullable numbers use `datatable.NullableNumber` or `expr.NullableOrderedColumn`, `NullableOrderFilter` and `NullableWhere`. Choose by the column's declared capabilities, not its underlying Go type. |
| Generated stable order | `Stable: []expr.ProjectionOrder[M]{expr.Asc(expr.Value(f.ID))}`. Use `f.Key()` when the declaration has no named ID carrier. |
| Generated projection and selection | A `role=row` DTO with one `datatable.Text`, `Number`, … or `datatable.Column` per field, and `Source` returning `rows.From(source)`. A projection that is not a table still uses `rows.Query[M,R]` and `rows.Select(rows.From(source), XRows, rows.Map(...))`, with `MapSelected` for computed values. |
| Generated joined-field sort | `expr.AsSelected(expr.Row(expr.At(joinedScope, field)))`. The Spec's first type parameter is the joined scope, not one model. |
| Correlated `CorrelatedScalarRowQuery(...).Value()` sort | `expr.AsSelected(expr.Row(rows.CorrelatedScalarRowQuery(...)))`; use `CorrelatedScalarNullableRowQuery` when the inner value is already nullable. |
| `datatable.Having` / `NullableHaving` | Supply typed adapters: `expr.OrderValueFilter(expr.Count[M]())` or `expr.NullableOrderValueFilter(expr.Sum(f.Amount))`. |
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
		return rows.SelectCorrelatedScalar(correlation, expr.AsSelected(expr.Row(expr.At(inner, countries.Countries.Name)))).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.Asc(expr.At(inner, countries.Countries.ISO2))).Limit(1)
	}
	sortedCountry := expr.AsSelected(expr.Row(rows.CorrelatedScalarRowQuery(correlateName())))
	statusFilter := datatable.NullableWhere(
		foundryhttp.EnumQuery[countries.Status, *countries.Status](countries.EnabledStatus.EnumDescriptor()),
		expr.NullableCompareFilter(expr.CompareNullableRow(expr.AtNullable(country, countries.Countries.Status))))
	latitudeFilter := datatable.NullableWhere(
		foundryhttp.FloatQuery[float64](),
		expr.NullableOrderFilter(expr.OrderNullableRow(expr.AtNullableValue(country, countries.Countries.Latitude))))
	spec.Columns = []datatable.ColumnRegistration[AuditScope, AuditRow]{
		datatable.HiddenSelected(validation.DefineBinding("id", func(r *AuditRow) *int64 { return &r.ID }), expr.AsSelected(expr.Row(expr.At(admin, Admins.Key())))),
		datatable.Column(validation.DefineBinding("country", func(r *AuditRow) *value.Nullable[string] { return &r.Country }), "audit.country", sortedCountry,
			datatable.SortBy(sortedCountry)),
		datatable.Column(validation.DefineBinding("status", func(r *AuditRow) *value.Nullable[countries.Status] { return &r.Status }), "audit.status", expr.AsSelected(expr.Row(expr.AtNullable(country, countries.Countries.Status))),
			datatable.FilterBy(statusFilter)),
		datatable.Column(validation.DefineBinding("latitude", func(r *AuditRow) *value.Nullable[float64] { return &r.Latitude }), "audit.latitude", expr.AsSelected(expr.Row(expr.AtNullableValue(country, countries.Countries.Latitude))),
			datatable.FilterBy(latitudeFilter)),
	}
	spec.Stable = []expr.ProjectionOrder[AuditScope]{expr.Asc(expr.At(admin, Admins.Key()))}
	spec.Source = func(ctx context.Context, actor A) (rows.SelectionSource[AuditScope], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return nil, err
		}
		return rows.WhereSource(joined, expr.Eq(expr.At(admin, Admins.TenantID), id)), nil
	}
	return spec
}
```

### Replace the projection declaration and Source callback

Before, documentation only:

<!-- orm-example: projection-source-before -->
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
`//foundry:dto role=row` in place of the projection directive, including its original
JSON names and response descriptor. Declare each field once through a table column;
there is no separate rows schema. Regenerate retained DTO output through the
Makefile; do not edit it. The standalone example below builds its bindings with
`validation.DefineBinding`. An application with a `role=row` DTO uses the generated
`AdminRowColumns()` bindings instead.

Use the generated `AdminTable` and `Admins` from the model directive. The
[declaration guide](model-declarations.md) owns the complete model recipe; keep
`AdminRow` as a separate response DTO with its existing JSON contract.

The column registrations now use selected expressions for sorts and row adapters
for filters, and each column selects its own field. `Hidden` selects the key into
the row without displaying it. Keep the application's other registrations, labels
and export codecs.

<!-- orm-example: pilot-columns -->
```go
func adminColumns() []datatable.ColumnRegistration[Admin, AdminRow] {
	id := validation.DefineBinding("id", func(r *AdminRow) *int64 { return &r.ID })
	username := validation.DefineBinding("username", func(r *AdminRow) *string { return &r.Username })
	email := validation.DefineBinding("email", func(r *AdminRow) *value.Nullable[string] { return &r.Email })
	return []datatable.ColumnRegistration[Admin, AdminRow]{
		datatable.Hidden(id, Admins.Key()),
		datatable.Text(username, "admins.fields.username", Admins.Username),
		datatable.NullableText(email, "admins.fields.email", Admins.Email),
	}
}
```

Apply mandatory tenant and visibility scopes before selecting the row. `OnlyTrashed`
and `WithTrashed` remain model-query operations and survive `rows.From`. The callback below receives an existing Spec so its authorization
and DTO descriptor remain caller-owned. The tenant resolver must use trusted actor
context. A UI filter must not replace that resolver.

<!-- orm-example: pilot-source -->
```go
func adminSource(tenantID int64, archived bool) rows.SelectionSource[Admin] {
	source := Admins.Query().Where(orm.Eq(Admins.TenantID, tenantID))
	if archived {
		source = source.OnlyTrashed()
	}
	return rows.From(source)
}

func adminTable[A any](spec datatable.Spec[Admin, AdminRow, A], tenant func(context.Context, A) (int64, error), archived bool) datatable.Spec[Admin, AdminRow, A] {
	// Retain the caller's Row descriptor, Authorize, ID, labels and export policy.
	spec.Columns = adminColumns()
	spec.Stable = []expr.ProjectionOrder[Admin]{expr.Asc(expr.Value(Admins.Key()))}
	spec.Source = func(ctx context.Context, actor A) (rows.SelectionSource[Admin], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return nil, err
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

`expr.Count` returns a required `int64`. Exact numeric `Sum` returns nullable
`decimal.Decimal`, including for integer input. Pass the present-value decimal
codec to `NullableHaving`, which supplies null operations. Keep the source's
GROUP BY and aggregate mappings; these filter constructors do not add grouping.
The [aggregate example above](#aggregates-windows-and-ctes) shows selected mappings.

<!-- orm-example: pilot-filter-families -->
```go
func orderGroupFilters() (datatable.FilterSource[models.Order, int64], datatable.FilterSource[models.Order, value.Nullable[decimal.Decimal]]) {
	count := expr.Count[models.Order]()
	total := expr.Sum(models.Orders.TotalCents)
	return datatable.Having(foundryhttp.IntegerQuery[int64](), expr.OrderValueFilter(count)),
		datatable.NullableHaving(foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), expr.NullableOrderValueFilter(total))
}

func relatedOrderFilter() datatable.FilterSource[models.User, int64] {
	return datatable.Related(
		datatable.Where(foundryhttp.IntegerQuery[int64](), expr.OrderFilter(expr.OrderRow(expr.Value(models.Orders.TotalCents)))),
		func(p expr.Predicate[models.Order]) expr.Predicate[models.User] {
			return expr.Has(models.UserRelations.Orders, p)
		})
}

func statusFilter() datatable.FilterSource[models.User, models.Status] {
	return datatable.Where(foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()),
		expr.CompareFilter(expr.CompareRow(expr.Value(models.Users.Status))))
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
	return f.exists(orm.Eq(countries.Countries.OfficialName, v))
}
func (f officialNameFilter[S]) Ne(v string) expr.Predicate[S] {
	return f.exists(orm.Ne(countries.Countries.OfficialName, v))
}
func (f officialNameFilter[S]) In(v ...string) expr.Predicate[S] {
	return f.exists(orm.In(countries.Countries.OfficialName, v...))
}
func (f officialNameFilter[S]) IContains(v string) expr.Predicate[S] {
	return f.exists(orm.IContains(countries.Countries.OfficialName, v))
}
func (f officialNameFilter[S]) IsNull() expr.Predicate[S] {
	return expr.Not(f.IsNotNull())
}
func (f officialNameFilter[S]) IsNotNull() expr.Predicate[S] {
	return f.exists(orm.IsNotNull(countries.Countries.OfficialName))
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
		return rows.SelectCorrelatedScalar(correlation, expr.AsSelected(expr.Row(expr.At(inner, countries.Countries.OfficialName)))).
			Where(expr.Equal(expr.At(inner, countries.Countries.ISO2), expr.At(outer, Admins.CountryCode))).
			OrderBy(expr.Asc(expr.At(inner, countries.Countries.ISO2))).Limit(1)
	}
	selected := expr.AsSelected(expr.Row(rows.CorrelatedScalarNullableRowQuery(correlate())))
	filter := officialNameFilter[DirectoryScope]{exists: func(p orm.Predicate[countries.Country]) expr.Predicate[DirectoryScope] {
		return correlate(p).Exists()
	}}
	countryName := expr.AsSelected(expr.Row(expr.At(country, countries.Countries.Name)))
	text := foundryhttp.StringQuery[string]()
	spec.Columns = []datatable.ColumnRegistration[DirectoryScope, DirectoryRow]{
		datatable.HiddenSelected(validation.DefineBinding("id", func(r *DirectoryRow) *int64 { return &r.ID }), expr.AsSelected(expr.Row(expr.At(admin, Admins.Key())))),
		datatable.Column(validation.DefineBinding("country", func(r *DirectoryRow) *string { return &r.Country }), "admins.country", countryName,
			datatable.SortBy(countryName),
			datatable.FilterBy(datatable.Where(text, expr.TextFilter(expr.TextRowAt(country, countries.Countries.Name)))),
		),
		datatable.Column(validation.DefineBinding("officialName", func(r *DirectoryRow) *value.Nullable[string] { return &r.OfficialName }), "admins.official_name", selected,
			datatable.SortBy(selected),
			datatable.FilterBy(datatable.NullableWhere(text, filter)),
		),
	}
	spec.Stable = []expr.ProjectionOrder[DirectoryScope]{expr.Asc(expr.At(admin, Admins.Key()))}
	spec.Source = func(ctx context.Context, actor A) (rows.SelectionSource[DirectoryScope], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return nil, err
		}
		return rows.WhereSource(joined, expr.Eq(expr.At(admin, Admins.TenantID), id)), nil
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
matching country; its official name can still be SQL NULL. The pilot tests assert
the SQL of both tables through `datatable.Compile`, not by compiling `Source`.

### Replace Foundry Countries factories and drafts

See [Countries](countries.md) for seeding, reference-data ownership and bounds.
The concrete `countries.Country`, `countries.Code` and `countries.Status` identities
remain unchanged. Import aliases such as `reference` do not change the mapping.

| Before | After |
| --- | --- |
| `reference.QueryFoundryCountries()` | `reference.Countries.Query()`. |
| `reference.CountryFields()` | `reference.Countries`. |
| `QueryFoundryCountries().Find(ctx, db, code)` | `orm.Find(ctx, db, Countries.Table, code)`. |
| `CountryDraft{}.SetStatus(status).SetConversionRate(rate)` | Separate assignments `orm.Set(Countries.Status, status)`, `orm.Set(Countries.ConversionRate, rate)`. |
| `QueryFoundryCountries().Update(ctx, tx, code, draft)` | `orm.Update(ctx, tx, Countries.Query(), Countries.Key(), code, assignments...)`. |

<!-- orm-example: pilot-countries -->
```go
func countryRead(ctx context.Context, db *database.DB, code countries.Code) (value.Optional[countries.Country], error) {
	country, err := orm.Find(ctx, db, countries.Countries.Table, code)
	if err != nil || !country.IsSet() {
		return country, err
	}
	f := countries.Countries
	_, err = orm.All(ctx, db.Primary(), f.Query().Where(orm.Eq(f.Status, countries.EnabledStatus)).
		OrderBy(orm.Asc(f.Name), orm.Asc(f.ISO2)).Limit(countries.MaxCountries))
	return country, err
}

func countryWrite(ctx context.Context, tx *database.Tx, code countries.Code, rate decimal.Decimal) error {
	f := countries.Countries
	_, err := orm.FirstLocked(ctx, tx, orm.Lock(f.Query().Where(orm.Eq(f.ISO2, code))))
	if err != nil {
		return err
	}
	_, err = orm.Update(ctx, tx, f.Table.Query(), f.Table.Key(), code, orm.Set(f.Status, countries.EnabledStatus), orm.Set(f.ConversionRate, rate))
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

<!-- orm-example: projection-source-before -->
<!-- orm-example: validation-signature-before -->
```text
func UniqueUsername[M any](executor database.Executor, source query.ModelQuerySource[M], column query.KeyField[M, string]) validation.Rule[string]
// caller: UniqueUsername(tx, models.QueryAdmins().WithTrashed(), models.AdminFields().Username)
```

The native source is `orm.Query[M]`; the field parameter is a queryable
`orm.ColumnHandle[M, string, I, P, K, R]` (constrained by `K orm.ColumnKind, R orm.ColumnRole`).
It admits any declared non-encrypted lookup-capable field such
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

func UniqueUsername[M, I, P any, K orm.ColumnKind, R orm.ColumnRole](executor database.Executor, source orm.Query[M], column orm.ColumnHandle[M, string, I, P, K, R], username validation.Rule[string]) validation.Rule[string] {
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
	_, err := orm.Paginate(ctx, db, models.Orders.Query(), orm.PageRequest{Number: 1, Size: 20})
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

Call the generated `user.FoundryIdentity()` or `user.FoundryReference()`;
identity methods are enabled unless the directive specifies `identity=false`.

Authentication and other `model.Identifiable` consumers use the generated identity
methods. Preserve custom identity semantics explicitly with `identity=false`.
Extension declarations reuse the generated identity
variable in the same package; slot fields infer their bindings from their types.

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
		_, err := orm.Insert(ctx, tx, models.Users.Table, orm.Set(models.Users.Email, "ada@example.test"))
		return err
	}}
}
```

`factory.Define(table, build)` now builds assignment slices; preserve factory states,
relationship composition and after-create behavior. Seeders must call the ordinary
write pipeline rather than reproducing conventions or lifecycle callbacks.

## Start new models and keep schema migrations

Use the [directive recipe](model-scaffolding.md), then run `foundry generate`.
Register the generated table with `application.WithTables`. Package-wide table
discovery is not automatic. `foundry make model` writes the directive struct and
prints the next generation command, just like the DTO scaffold.

DTO, enum and config generation continue independently. Migration scaffolds,
schema SQL and seed definitions are unchanged. See
[migrations and seeding](migrations-and-seeding.md#development-scaffolding).

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

For a locked lookup by key, use `Where` with a typed key predicate and `Require`:

<!-- orm-example: pilot-recipe-locked-find -->
```go
func requireAdminForUpdate(ctx context.Context, tx *database.Tx, id int64) (Admin, error) {
	return orm.RequireLocked(ctx, tx, orm.Lock(Admins.Query().Where(orm.Eq(Admins.Key(), id))))
}
```

Locks are available only on `LockedQuery` inside an active transaction (`*database.Tx`). Use `Require` to return `database.NotFound` on absence, or `First` when absence is handled with `value.Optional[M]`.

### Model reference and identity

The model directive generates `FoundryReference` and `FoundryIdentity` by default.
They preserve the concrete stored key and its identity codec. Do not duplicate
these methods in application source. `identity=false` preserves an intentional
custom identity contract. See [identity](model-declarations.md#recipe-register-tables).

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
		orm.Set(Admins.TenantID, tenantID),
		orm.Set(Admins.Username, username),
	}
	assignments = orm.SetIfPresent(assignments, Admins.Email, email)
	return orm.Insert(ctx, tx, Admins.Table, assignments...)
}
```

Omitted fields are not passed to `Insert`, allowing database default values to apply. To explicitly write a SQL NULL on a nullable column, use the nullable family function, such as `orm.SetNull(column)`.

## Use native shared contracts and limits

Page and cursor contracts live under `orm`: `PageRequest`, `PageResult[R]`,
`SimplePage[R]`, `Cursor[R]`, `CursorRequest[R]`, `CursorPage[R]`,
`CursorInputError` and `ParseCursor[R]`. The total-bearing page uses the name
`PageResult`, while its JSON fields retain their existing names. Parsing retains
the same bounds, token format and redacted errors.

Use `model.MorphName` and `model.Morphable` for polymorphic model declarations,
`orm.RelationLimits` / `orm.DefaultRelationLimits` for eager-loading budgets,
and `database.JSONKind` for a DTO field describing a JSON value's kind.

Application batching and query builders can use `orm.MaxInsertRows`,
`MaxPerModelWriteRows`, `MaxRelationWriteRows`, `MaxIdentityBatch`, `MaxChunkSize`,
`DefaultChunkSize`, `MaxPageSize`, `MaxCursorBytes`, `MaxCursorFields`,
`MaxExpressionDepth`, `MaxExpressionNodes`, `MaxParameters`,
`MaxMembershipValues` and `MaxScalarSQLBytes` (all in `orm`). These refer to the
same compiler limits. See [consumer resource limits](../../database/orm/README.md#consumer-resource-limits)
for each bound's scope. No application should import framework `internal` packages
or maintain a copied constant to bound its inputs.

## Preserve the remaining behavior

Numeric Increment/Decrement use positive finite magnitudes; AtMost bounds increments
and AtLeast bounds decrements. Keys, managed timestamps and input columns cannot gain
adjustment capability. Use PruneBatch or ForcePruneBatch with explicit
bounds and mass/per-model selection; the prune registry exposes ORMModel/ORMSoftModel.
Explain is read-only planning; ExplainAnalyze executes the statement. Typed explain
results and errors are redacted by default; explicit JSON/root access exposes plans.

Keep encrypted constructors and stored codecs. Queries cannot compare, sort or project
encrypted fields. orm.Raw validates complete hydration and bypasses scopes; executor
raw APIs remain explicit escape hatches. Preserve the project's no-raw policy.

The [breaking-change notes](../evidence/goway-breaking-changes.md) distinguish shipped
syntax changes, startup validation and the remaining publication prerequisites.

The before snippets above are historical, documentation-only examples. The after
snippets remain compiled by the migration fixture after retirement of generated query APIs.

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
	enabled := expr.When(expr.Eq(expr.Value(Admins.Username), "active"),
		expr.Constant[Admin](codec.Bool[bool](), true)).Else(expr.Constant[Admin](codec.Bool[bool](), false))
	return rows.SelectScalar(rows.From(Admins.Query()), expr.AsSelected(enabled))
}
func scopedUsername() {
	type adminScope struct{}
	source := rows.As[adminScope](Admins.Query(), "admin")
	parameter := expr.ParamOf(expr.Row(expr.At(rows.Scope(source), Admins.Username)), "Ada")
	_ = parameter
}
```

`rows.BoolOf` selects a Boolean row codec. Model Boolean fields infer their
codec; mutated fields declare `input=Type`. Framework enums already expose enum descriptors: use `rows.EnumOf` for
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
collection-name argument to `attachments.ManyOf` or `attachments.OneOf`. Known slot types infer their generated bindings without tags. `extensions.BindSlot`
retains owner/key/value typing. Keep the collection name, extension owner identity
and `StorageModel` unchanged when renaming the Go field. The example binds Gallery
to the existing public-media collection; no stored-data migration is needed.

Pass the existing `"public-media"` collection name to `attachments.ManyOf` with
`extensions.BindSlot(ext.Articles.Key(), ext.Articles.Gallery)`. The directive
generates `Gallery` from its attachment type; keep that stored name in the policy.

See [extension slots](model-extension-slots.md), [JSON models](json-models.md) and
[projection declarations](model-projections.md) for the owning contracts.

## v0.3.0 expression and datatable migration

Row expression operators become package functions. Datatable columns take typed
options and return `ColumnRegistration[S, R]` directly. Remove the old method
calls; there are no compatibility methods.

### Row operators

| From (v0.2) | To (v0.3) |
| --- | --- |
| `e.Eq(v)`, `e.Ne(v)`, `e.In(vs...)` | `expr.Eq(e, v)`, `expr.Ne(e, v)`, `expr.In(e, vs...)` |
| `opt.Eq(v)`, `opt.Ne(v)`, `opt.In(vs...)` for present values | `expr.NullableEq(opt, v)`, `expr.NullableNe(opt, v)`, `expr.NullableIn(opt, vs...)` |
| `e.Lt(v)`, `e.Lte(v)`, `e.Gt(v)`, `e.Gte(v)` | `expr.Lt(e, v)`, `expr.Lte(e, v)`, `expr.Gt(e, v)`, `expr.Gte(e, v)` |
| `opt.Lt(v)`, `opt.Lte(v)`, `opt.Gt(v)`, `opt.Gte(v)` | `expr.NullableLt(opt, v)`, `expr.NullableLte(opt, v)`, `expr.NullableGt(opt, v)`, `expr.NullableGte(opt, v)` |
| `e.IsNull()`, `e.IsNotNull()` | `expr.IsNull(e)`, `expr.IsNotNull(e)` |
| `t.Contains(s)`, `t.IContains(s)` | `expr.TextContains(t, s)`, `expr.TextIContains(t, s)` |
| `t.StartsWith(s)`, `t.EndsWith(s)` | `expr.TextStartsWith(t, s)`, `expr.TextEndsWith(t, s)` |
| `t.IStartsWith(s)`, `t.IEndsWith(s)` | `expr.TextIStartsWith(t, s)`, `expr.TextIEndsWith(t, s)` |
| `t.Like(p)`, `t.ILike(p)` | `expr.TextLike(t, p)`, `expr.TextILike(t, p)` |
| Nullable text operations, such as `optT.Contains(s)` | Add `Nullable` to the function name: `expr.NullableTextContains(optT, s)`; the same rule applies to all eight text operations above |
| `e.Asc()`, `e.Desc()` for row values | `expr.Asc(e)`, `expr.Desc(e)` |
| `e.Asc().NullsFirst()`, `e.Desc().NullsLast()` | `expr.Asc(e).NullsFirst()`, `expr.Desc(e).NullsLast()` |

The names use the operator suffixes and `Nullable`/`Text` prefixes established by
`orm` column functions. Equality and membership use `Eq`, `Ne` and `In` because
`RowValue[S, V]` already carries the exact value type; there is no parallel
`TextEq` spelling in `expr`.

Ordered comparisons require `OrderedRowValue[S, V]`. Ordered calculations and
text expressions carry this sealed capability. Use the existing `OrderRow` or
`OrderNullableRow` constructor when starting from a general row value.
Text operations require `TextRowExpression` or `NullableTextRowExpression`;
`CompareRow` and `CompareNullableRow` do not grant enum text or ordered operations.
Nullable operators compare a present `V` while retaining the expression's
`value.Nullable[V]` result. `Eq`/`Ne`/`In` on a general nullable `RowExpression`
still take its exact wrapped value type.

### Selected and aggregate operators

| From (v0.2) | To (v0.3) |
| --- | --- |
| `row.Value()`, `aggregate.Value()` | `expr.AsSelected(row)`, `expr.AsSelected(aggregate)` |
| `e.Param(v)` on a row, selected, or aggregate exemplar | `expr.ParamOf(e, v)`; column parameters remain `expr.Param(column, v)` |
| `selected.Key()` | `expr.Key(selected)` |
| `aggregate.Filter(predicates...)` | `expr.Filter(aggregate, predicates...)` |
| `a.Eq(v)`, `a.Ne(v)`, `a.In(vs...)` in HAVING | `expr.EqValue(a, v)`, `expr.NeValue(a, v)`, `expr.InValue(a, vs...)` |
| `a.Lt(v)`, `a.Lte(v)`, `a.Gt(v)`, `a.Gte(v)` in HAVING | `expr.LtValue(a, v)`, `expr.LteValue(a, v)`, `expr.GtValue(a, v)`, `expr.GteValue(a, v)` |
| `a.IsNull()`, `a.IsNotNull()` in HAVING | `expr.IsNullValue(a)`, `expr.IsNotNullValue(a)` |
| `a.Asc()`, `a.Desc()` on selected values or aggregates | `expr.AscValue(a)`, `expr.DescValue(a)` |
| `having.Not()` | `expr.HavingNot(having)` |
| `text.Like(p)`, `text.ILike(p)` in HAVING | `expr.TextLikeValue(text, p)`, `expr.TextILikeValue(text, p)` |
| `text.Contains(v)`, `text.IContains(v)` in HAVING | `expr.TextContainsValue(text, v)`, `expr.TextIContainsValue(text, v)` |
| `text.StartsWith(v)`, `text.EndsWith(v)` in HAVING | `expr.TextStartsWithValue(text, v)`, `expr.TextEndsWithValue(text, v)` |
| `text.IStartsWith(v)`, `text.IEndsWith(v)` in HAVING | `expr.TextIStartsWithValue(text, v)`, `expr.TextIEndsWithValue(text, v)` |

Selected comparisons use the same spelling for required and nullable operands.
Nullable operands still bind the present value through their declared nullable
codec. `CompareValue`, `OrderValue`, `OrderNullableValue`, `TextValue`, and
`TextNullableValue` retain their comparison capabilities and exact value types.
`Filter` returns the same concrete aggregate type; its predicates remain in the
row phase and must have the aggregate's scope. `AsSelected` retains result nullability.
`ParamOf` on a nullable aggregate uses its present-value codec for fallbacks.

Selected comparisons produce `HavingPredicate[S]`; row comparisons produce
`Predicate[S]`. `AscValue` and `DescValue` produce `SelectedOrder[S]`; row ordering
functions produce `Order[S]`. These phases remain distinct. Window, frame, CASE,
and JSON-path builders remain methods on exported non-alias types.

`Order[S]` and `SelectedOrder[S]` are exported non-alias types. Both retain
`NullsFirst()` and `NullsLast()` methods, visible in `go doc`, so ordering reads
`expr.Asc(e).NullsFirst()` or `expr.DescValue(a).NullsLast()`.

Public expression types retain their scope, value and phase parameters. Most
implementations use public aliases to private types with pointer-shaped scope
parameters, for example `TextRowExpression[S, V] = textRowExpression[*S, V]`.
Applications continue to name the public types; distinct aliases, joins and
correlations retain distinct source scopes.

Editor hover and compiler errors can show internal pointer-scoped names for expression
types, such as `expr.predicate[*models.Order]` or
`expr.selected[*models.Order, int64]`. The pointer is an internal scope type
argument. Applications still use the public aliases, such as
`expr.Predicate[models.Order]` and `expr.Selected[models.Order, int64]`.
The public types and their source, value, phase, and nullability guarantees are
unchanged.

### Datatable filter adapters

| From (v0.2) | To (v0.3) |
| --- | --- |
| `datatable.Where(codec, text)` | `datatable.Where(codec, expr.TextFilter(text))` |
| `datatable.NullableWhere(codec, optText)` | `datatable.NullableWhere(codec, expr.NullableTextFilter(optText))` |
| `datatable.Where(codec, ordered)` | `datatable.Where(codec, expr.OrderFilter(ordered))` |
| `datatable.NullableWhere(codec, optOrdered)` | `datatable.NullableWhere(codec, expr.NullableOrderFilter(optOrdered))` |
| `datatable.Where(codec, comparison)` | `datatable.Where(codec, expr.CompareFilter(comparison))` |
| `datatable.NullableWhere(codec, optComparison)` | `datatable.NullableWhere(codec, expr.NullableCompareFilter(optComparison))` |
| `datatable.Having(codec, aggregate)` | `datatable.Having(codec, expr.OrderValueFilter(aggregate))` for ordered aggregates; `expr.ValueFilter(aggregate)` for equality-only aggregates |
| `datatable.NullableHaving(codec, aggregate)` | `datatable.NullableHaving(codec, expr.NullableOrderValueFilter(aggregate))` |
| Selected text comparisons in HAVING filters | `expr.TextValueFilter(comparison)` or `expr.NullableTextValueFilter(comparison)` |

The adapters retain the expression's source, value, nullability, and phase.
`ValueFilter` and `NullableValueFilter` expose equality and membership;
`OrderValueFilter` and `NullableOrderValueFilter` add ordered comparisons;
`TextValueFilter` and `NullableTextValueFilter` add text operations. Their methods
are declared on exported non-alias adapter types and remain visible in `go doc`.

### Datatable column options

| From (v0.2) | To (v0.3) |
| --- | --- |
| `column.SortBy(selected)` | `datatable.SortBy(selected)` in `datatable.Column` |
| `column.FilterBy(source)` | `datatable.FilterBy(source)` in `datatable.Column` |
| `column.Searchable()` | `datatable.Searchable(rowExpression)` in `datatable.Column` |
| `column.ExportAs(cell)` | `datatable.ExportAs(selectedExpression, cell)` in `datatable.Column` |
| `column.Registration()` | Use the `datatable.Column` result directly |

<!-- orm-example: column-options-v030-before -->
```go
column := datatable.DefineColumn[UserScope](w.Username, key).
	SortBy(username.Value()).
	FilterBy(datatable.Where(Text, username)).
	Searchable().
	ExportAs(datatable.ScalarCell(Text)).
	Registration()
```

The [compiled column declaration](../../tests/fixtures/goway/migration/column_options_test.go)
uses the same expression to infer the option types:

<!-- orm-example: column-options-v030 -->
```go
func usernameColumn() datatable.ColumnRegistration[Admin, AdminRow] {
	text := foundryhttp.StringQuery[string]()
	field := validation.DefineBinding("username", func(row *AdminRow) *string { return &row.Username })
	username := expr.TextRow(Admins.Username)
	return datatable.Column(field, "admins.fields.username", expr.AsSelected(username),
		datatable.SortBy(expr.AsSelected(username)),
		datatable.FilterBy(datatable.Where(text, expr.TextFilter(username))),
		datatable.Searchable(username),
		datatable.ExportAs(expr.AsSelected(username), datatable.ScalarCell(text)),
	)
}
```

`ColumnOption[S, V]` retains both types through registration without assertions or
value boxing. `datatable.Column[S, R, V]` ties each option to the DTO selector's exact
`V` and the declared source `S`. `SortBy` takes `Selected[S, V]`; `FilterBy` takes
`FilterSource[S, V]`. `Searchable` takes `RowValue[S, V]` to infer its two types.
`ExportAs` takes `Selected[S, V]` and `Cell[V]`, inferring both types without
repeating the scope. For an aggregate column, use `ExportAs(expr.AsSelected(count), cell)`.
The search/export expression supplies types only; export formatting reads the DTO
field. Report/table `Registration()` methods remain unchanged.
