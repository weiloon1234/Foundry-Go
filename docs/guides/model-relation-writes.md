# Typed relation writes

`orm.Attach`, `orm.Detach` and `orm.ForceDetach` use the keys already declared by `orm.ManyToManyOf`. Source models, target models and pivot drafts retain their concrete Go types. The [independent consumer](../../tests/fixtures/consumer/linkqueries/models.go) defines a member/group relationship with a typed `Membership` pivot; no application-side column maps or raw queries are needed.

## Attach a model

Here `models` is the application model package. Its `MemberLinks.Groups`
relation and generated `Memberships` and `Groups` tables come from the
[typed declarations](../../tests/fixtures/consumer/linkqueries/orm.go).

The application owns `models.Member`, `models.Group`, `models.Membership`, `Document`, `Tag`, and `DocumentTag` and any generated tables or identities used below.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database`, `model`, `database/orm`, `value`.

Inputs: `ctx context.Context`, `db *database.DB`, `member models.Member`, `group models.Group`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_relation_writes.go#model-relation-writes-1 -->
```go
groups := models.MemberLinks.Groups
membership, err := orm.Attach(ctx, db, groups, member, group,
	orm.Set(models.Memberships.Role, " ADMIN "))
if err != nil {
	return err
}
```

`membership` is a complete `Membership` model. Foundry refreshes the stored member and group by their typed primary identities, then derives the pivot keys from the relationship metadata. Omitted draft keys receive those defaults before the usual UUID preparation and creating/saving hooks. The pivot's `MutateRole` setter, managed timestamps and local/provider observers run through the same pipeline as ordinary `orm.Insert`. The original draft and supplied endpoint structs remain unchanged.

Explicit draft inputs remain explicit. A hook or setter can transform them, but the stored result must still link the selected endpoints and satisfy the relationship's target/pivot filters. A failed postcondition rolls back the relation write and its queued after-commit callbacks. A distinct custom setter input cannot be manufactured from a stored key; supply that typed input in the draft when necessary. Typed `orm.Assignment[P]` values carry input and owner roles into the shared write pipeline.

Repeated attachments create separate pivots unless a database constraint rejects the duplicate. Declare the domain's uniqueness policy in a migration. A uniqueness violation returns the ordinary classified database error and rolls back the attempted link.

## Detach and force detach

Inputs: `ctx context.Context`, `db *database.DB`, `member models.Member`, `group models.Group`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_relation_writes.go#model-relation-writes-2 -->
```go
groups := models.MemberLinks.Groups
removed, err := orm.Detach(ctx, db, groups, member, group)
if err != nil {
	return err
}
permanentlyRemoved, err := orm.ForceDetach(ctx, db, groups.WithTrashedPivot(),
	models.Memberships.Soft, member, group)
if err != nil {
	return err
}
```

Both results are `[]Membership`, one complete model for each selected pivot. `Detach` runs normal per-model deletion: soft-delete pivots remain stored, while ordinary pivots are physically removed. Soft deletion returns the stored post-write model; physical deletion returns its pre-write model. Duplicate links invoke separate hooks. All selected removals commit or roll back together, including when a later pivot hook fails. Detachment shares its bounded candidate selection and ordinary mutation runtime with [per-model batch writes](model-batch-writes.md).

`ForceDetach` requires a soft-delete pivot and runs its force-delete lifecycle. The selected pivot visibility remains in force: use `WithTrashedPivot` to include deleted links, or `OnlyTrashedPivot` to select only deleted links. Ordinary `Detach` changes active pivots even with `WithTrashedPivot`; `orm.Detach(..., groups.OnlyTrashedPivot(), ...)` therefore selects nothing. To restore a known pivot, use its ordinary `orm.Restore` operation with the typed pivot ID. See [soft deletion](model-soft-deletes.md) for event order and visibility.

## Synchronizing many links

`Document` and `Tag` are the application's models. Their many-to-many relation
stores its link data in this application-owned `DocumentTag` model:

<!-- compiled-source: tests/fixtures/consumer/tenantqueries/models.go#document-tag-pivot -->
```go
//foundry:model table=tenant_document_tags var=DocumentTags key=ID identity=false
type DocumentTag struct {
	ID         model.ID[DocumentTag]
	DocumentID model.ID[Document] `foundry:"role=column"`
	TagCode    TagCode
	Weight     int64
	CreatedAt  time.Time `foundry:"role=created-at"`
	UpdatedAt  time.Time `foundry:"role=updated-at"`
}
```

The model directive generates `DocumentTagTable` and the `DocumentTags` value.
[The generated table type](../../tests/fixtures/consumer/tenantqueries/models_foundry.gen.go)
embeds `orm.Table[DocumentTag, model.ID[DocumentTag]]`, exposes the typed
`DocumentID` and `TagCode` columns, and gives `Weight` the type
`orm.Number[DocumentTag, int64]`. Its `CreatedAt` and `UpdatedAt` fields carry
the managed timestamp roles. The function receives this generated table and the
typed `DocumentLinks` relation explicitly; it never constructs a second table binding.

Inputs: `ctx context.Context`, `db *database.DB`, `document Document`, `goTag Tag`, `sqlTag Tag`, `DocumentLinks orm.ThroughRelation[Document, Tag, DocumentTag]`, `DocumentTags DocumentTagTable`.

<!-- compiled-source: tests/fixtures/consumer/tenantqueries/guide_sync.go#model-relation-writes-3 -->
```go
tags := DocumentLinks
changes, err := orm.Sync(ctx, db, tags, document, []Tag{goTag, sqlTag},
	[]orm.Assignment[DocumentTag]{orm.Set(DocumentTags.Weight, 1)},
	[]orm.Assignment[DocumentTag]{orm.Set(DocumentTags.Weight, 5)})
if err != nil {
	return err
}
```

The [tenant consumer](../../tests/fixtures/consumer/tenantqueries/tenant_postgres_test.go) exercises every operation:

| Package function (first arguments: ctx, db, relation) | Effect |
| --- | --- |
| `orm.AttachMany(ctx, db, relation, source, targets, create)` | creates one pivot per distinct target; like `Attach`, repeated calls create duplicate links unless a constraint rejects them |
| `orm.Sync(ctx, db, relation, source, targets, create, update)` | links exactly the targets: missing links created, other links removed, retained links updated when `update` is non-nil |
| `orm.SyncWithoutDetaching(ctx, db, relation, source, targets, create, update)` | like `Sync` without removing other links |
| `orm.Toggle(ctx, db, relation, source, targets, create)` | links unlinked targets and removes the links of linked ones |
| `orm.DetachMany(ctx, db, relation, source, targets)` / `orm.DetachAll(ctx, db, relation, source)` | removes the source's links to the targets, or all of its links |
| `orm.UpdatePivot(ctx, db, relation, source, target, update)` | updates the source's links to one target; `database.NotFound` when none exist |

`Sync`, `SyncWithoutDetaching` and `Toggle` return a typed `orm.PivotChanges[DocumentTag]` with complete `Attached`, `Detached` and `Updated` pivot models; each carries its typed source and target keys. The other methods return the affected pivots. `create` and `update` are typed assignment slices. Omitted keys are filled from the relationship as for `Attach`; a nil update leaves retained pivots unchanged.

Each call is one transaction (or one savepoint of the caller's). The source and all targets are refreshed and locked within their filters in one query each, and a missing target fails before any link changes. `Sync`, `SyncWithoutDetaching`, `Toggle`, `DetachMany`, `DetachAll` and `UpdatePivot` lock the source row exclusively (`FOR NO KEY UPDATE`), so concurrent calls for one source run one after another and each reads the links the previous one committed: two concurrent `Sync`s leave exactly the later call's set, and two `Toggle`s never create duplicate links. `AttachMany` keeps a shared source lock, so concurrent attachments proceed in parallel and rely on a unique pivot constraint to reject duplicates. The exclusive lock does not block ordinary reads or foreign-key checks against the source ([concurrency acceptance](../../tests/fixtures/consumer/tenantqueries/pivot_concurrency_postgres_test.go)). Current links are read once and locked. Pivot filters (`WherePivot`) and pivot scopes select which existing links count. Without pivot hooks or registered observers, the changes run as a few set-based statements: one multi-row `INSERT`, one `UPDATE`/`DELETE ... RETURNING` per change kind (soft-delete pivots are soft-deleted), and one query verifying that every created pivot satisfies the relationship's endpoints and filters. Managed timestamps and field mutators apply. When the pivot declares hooks or has registered observers, each changed pivot instead runs its ordinary create, update or delete lifecycle in the same transaction, exactly as `Attach` and `Detach` do: once per changed link, with its before/after state, and never for retained links that are not updated. A hook failure rolls back the whole call, including links already changed, and no after-commit callbacks run ([hook acceptance](../../tests/fixtures/consumer/pivothooks/pivot_hooks_postgres_test.go)). Bounds follow `orm.WithRelationWriteLimit`.

## Filters, keys and bounded writes

Inputs: `ctx context.Context`, `db *database.DB`, `member models.Member`, `group models.Group`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_relation_writes.go#model-relation-writes-4 -->
```go
adminGroups := models.MemberLinks.Groups.
	Where(orm.Eq(models.Groups.Name, "staff")).
	WherePivot(orm.Eq(models.Memberships.Role, "admin"))
adminGroups = orm.WithRelationWriteLimit(adminGroups, 50)
removed, err := orm.Detach(ctx, db, adminGroups, member, group)
if err != nil {
	return err
}
```

Target and pivot filters retain their model ownership. Endpoint arguments identify existing rows; stale non-primary relation keys are refreshed before deriving the link. A missing or filtered-out endpoint returns `database.NotFound`. The source must be active. Target `WithTrashed`/`OnlyTrashed` visibility is explicit and independent of pivot visibility. NULL endpoint relation keys fail with `fault.Missing`. A non-primary target key matching multiple targets within the relation scope returns `database.TooManyRows`; migrations must enforce uniqueness when concurrent inserts could create ambiguity.

Detach locks and reads the matching pivot set in primary-key order before calling a pivot hook. `orm.MaxRelationWriteRows` shares the existing atomic insert row budget. `orm.WithRelationWriteLimit` can lower that bound; an oversized set fails before any pivot mutation rather than returning a truncated success. It does not alter read limits. Ordering and eager-loading options are rejected for writes; use a descriptor containing the required filters and visibility.

## Transactions and concurrency

Relation writes accept the same `database.Transactor` capability as ordinary model writes, including a pool, a transaction or a wrapper. Foundry owns the transaction/savepoint for the operation. A veto, SQL/codec error, cancellation or failed stored-link check rolls back its database changes and pending after-commit work. An enclosing transaction still owns the final commit. If commit has an unknown outcome or an after-commit callback fails after a confirmed commit, the ordinary typed `orm.WriteError[P]` (or `orm.WriteError[[]P]` for detachment) retains the candidate result; inspect the outcome before deciding whether to retry. See [model write outcomes](model-writes.md). Callback side effects outside that database transaction retain the ordinary [hook limitations](model-hooks.md).

Source and target rows are refreshed under shared row locks, held through the relation transaction. When the caller supplies a transaction, those locks remain until its outer commit or rollback. Internal ownership reads skip `Retrieved` callbacks. Detach locks its complete bounded candidate set before mutation; it does not claim to remove links inserted after that selection. Hook code may perform more database work in the same transaction, and ordinary database deadlock/conflict errors remain possible. A pre-write cardinality check does not replace physical uniqueness constraints.

Attach validates the actual stored pivot through the existing relationship-existence AST after its normal write pipeline. This covers explicit key inputs, transformed values and scoped links without introducing a separate SQL compiler. Detach invokes the same single-model mutation pipeline for each locked candidate; hooks, changes and after-commit behavior are not duplicated in a relation-specific implementation.

## Acceptance coverage

The [lifecycle consumer](../../tests/fixtures/consumer/linkqueries/lifecycle_postgres_test.go) covers derived inputs, mutators, timestamps, hook order, effective changes, duplicate links, row limits, soft/force deletion, veto rollback, cancellation and outer-commit callbacks. The [key consumer](../../tests/fixtures/consumer/linkqueries/keys_postgres_test.go) exercises stale natural keys, ambiguous/nullable keys, self-links, visibility and transactor wrappers. The [concurrency consumer](../../tests/fixtures/consumer/linkqueries/concurrency_postgres_test.go) checks endpoint locks through hooks and competing attachments under a unique constraint. Compiler failures reject wrong source/target models, pivot drafts, result models and default ownership; real gopls probes inspect the concrete public signatures.
