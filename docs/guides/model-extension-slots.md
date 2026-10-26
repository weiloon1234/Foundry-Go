# Model extension slots

Declare translations, attachments and metadata as ordinary model fields, bind their
offsets with `orm.SlotOf`, and declare their policies outside `orm.Define`. Slots
store data in shared framework tables, not columns in the model's own table.
The [articles consumer](../../tests/fixtures/consumer/articles/tables.go) contains
the full declaration and HTTP workflow. See [ORM migration](orm-migration.md) for
the old-to-new mapping.

## Declare slots on the model

The [compiled extension example](../../tests/fixtures/goway/models/extensions/models.go)
uses an integer key and four slot kinds:

<!-- compiled-source: tests/fixtures/goway/models/extensions/models.go -->
```go
type Article struct {
	ID      int64
	Title   translations.Text
	Cover   attachments.One[Article]
	Gallery attachments.Many[Article]
	SEO     metadata.Value[string]
}
```

Its table namespace exposes `orm.Slot[Article, V]` handles; `V` is the exact
slot field type. Bind each with `orm.SlotOf(b, &m.Field)` inside `orm.Define`.
The owner derives from the table and its primary key:

<!-- compiled-source: tests/fixtures/goway/models/extensions/models.go -->
```go
var Owner = extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table, Articles.Key()), extensions.OwnerOptions{StorageModel: "extension_articles"})
```

`attachments.One[M]` and `attachments.Many[M]` must use the enclosing model type.
Slot bindings retain the concrete owner, key and value. A translation handle cannot
load an attachment slot, and a handle for one owner cannot be used on another.
Slots are excluded from parent SQL, assignments and automatic column audit.

## Declare policies outside the table

The guide binds policies once below. Here, `ext` is the compiled extension model
package linked above:

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go -->
```go
// ArticleExtensions binds policies to the exact stored slot types.
// Example: ArticleExtensions.From(runtime).
var ArticleExtensions = ext.Descriptors{
	Title:   translations.TextOf(ext.Owner, "title", translations.Options{}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Title)),
	Cover:   attachments.OneOf(ext.Owner, "cover", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Cover)),
	Gallery: attachments.ManyOf(ext.Owner, "gallery", attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}}, extensions.BindSlot(ext.Articles.Key(), ext.Articles.Gallery)),
	SEO:     metadata.ValueOf(ext.Owner, "seo", metadata.Spec[string]{}, contract.StringJSON[string](), extensions.BindSlot(ext.Articles.Key(), ext.Articles.SEO)),
}
```

`TextOf`, `OneOf`, `ManyOf` and `ValueOf` produce thin typed handles backed by
shared state. Their `From` methods borrow the existing translation, attachment
and metadata managers. Group them in an application-owned namespace whose `From`
and `All` methods compose those handles. The [compiled declaration](../../tests/fixtures/goway/models/extensions/models.go)
shows both methods; `All` returns `[]orm.Relation[Article]`, built with each
handle's `Relation(tableSlot)`.

| Field | Shared storage | Policy |
| --- | --- | --- |
| `translations.Text` | `foundry_model_translations` | `translations.Options`; default 64 KiB per value and no required locale |
| `attachments.One[M]`, `attachments.Many[M]` | `foundry_attachments` and a disk | `attachments.Policy`; a disk and accepted media types or an image plan are required |
| `metadata.Value[V]` | `foundry_model_metadata` | `metadata.Spec[V]` and an explicit JSON descriptor; default version 1 |

The example accepts PNG files; configure the named disk before assembly. Cardinality
comes from the handle kind; a conflicting policy is invalid. Localized collections
use the explicit `attachments.Define` and `ForLocale` path, not slot policies.

`translations.Options.Require` accepts `i18n.OptionalLocales`, `i18n.DefaultLocale`
or `i18n.AllLocales`. Complete input rules and exact synchronization enforce it;
merge operations retain other locales. A metadata value uses the same generated
DTO codec or explicit `contract` descriptor as its public value contract. Adding
optional DTO fields needs no storage migration. Removing or renaming fields needs
a metadata version change because strict decoding rejects stored unknown fields.

## Stored names and owner identity

The owner name, `StorageModel`, and each slot's name are explicit persisted identity.
Keep these literals stable when renaming a Go field or table. A Go field rename
alone does not move slot data. Changing `"title"` to a different stored name hides
its old rows until you restore the name or migrate the data.

Stored names use lowercase letters, digits, `_`, `.` or `-`, and are unique across
the owner's slots. See [owner renames and recovery](model-extensions.md#owner-identity-and-table-renames)
for `StorageModel`, `PreviousModels` and maintenance commands. Attachment object
paths embed the scope, so declare the stable storage model before renaming a table.

## Application assembly

Return `slots.DeclareORM(owner, parts)` from an application-owned declaration
function. `parts` lists the handles' `Registration()` and `Describe()` values:

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go -->
```go
func articleDeclaration() slots.Declaration {
	return slots.DeclareORM(ext.Owner, slots.Parts{
		Translations: []translations.Registration{ArticleExtensions.Title.Registration()},
		Attachments:  []attachments.Registration{ArticleExtensions.Cover.Registration(), ArticleExtensions.Gallery.Registration()},
		Metadata:     []metadata.Registration{ArticleExtensions.SEO.Registration()},
		Slots:        []extensions.SlotDescription{ArticleExtensions.Title.Describe(), ArticleExtensions.Cover.Describe(), ArticleExtensions.Gallery.Describe(), ArticleExtensions.SEO.Describe()},
	})
}
```

Pass that declaration to `application.Builder.Models`. Register the model's checked
tables separately through `application.WithTables`. Assembly rejects duplicate
registrations and missing feature dependencies before acquiring resources. Every
slot needs extensions; translated slots need locales, and attachments need storage
and attachments enabled.

Bind managers once in a constructor using `services.ModelExtensions()` and your
namespace's `From(runtime)`. A nil manager leaves its handle unbound. Direct
foundation assembly registers the declaration's cleanup observers for every pool
that can delete its owner and supplies `slots.Runtime.Store` for cross-pool cleanup.

## Loading slots

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go -->
```go
func articles(ctx context.Context, db database.Executor, runtime slots.Runtime) ([]ext.Article, error) {
	bound := ArticleExtensions.From(runtime)
	return ext.Articles.Query().With(bound.All()...).All(ctx, db)
}
```

A bound handle's `Relation` participates in `With`, nested relations, `orm.LoadFrom`
and `orm.LoadMissingFrom`. Both loading functions honor the source query's relation
limits. `Count` and `Exists` do not load slots.
Repeated slots fail validation; unbound handles fail before parent SQL.

Each slot uses the manager's batch queries per parent batch: an active-owner check
and text pages, files and variants, or metadata values. Relation batches default
to 500 parents. Attachment batches also respect the 4096-file bound. Oversized
row or byte batches split and retry, until each owner fits or fails alone.
Fetched and attached values consume the query's relation budget, including depth.
Load only the slots a response needs.

A parent read through a `*database.Tx` on the store's pool lets slot reads join
that transaction through a savepoint. Other executors use the store's read-only
snapshot. Parent reads and slot reads remain separate statements. Inactive owners,
including soft-deleted models read with `WithTrashed`, keep unloaded slots.

## Reading loaded slots

Reads never perform I/O. Loaded state distinguishes an unloaded slot from a loaded
empty value. Translations expose `Exact`, `Resolve`, `ResolveRequest` and `Values`.
Their fallback is the requested locale, supported regional parents, catalog default,
then remaining supported locales. Empty text is a present value.

`attachments.One.Get` and `metadata.Value.Get` return the optional value and a
loaded flag. `attachments.Many` exposes `Get`, `Len` and ordered `All`.
Attachment handles derive `PublicURL`, `PublicURLs`, `URL`, `TemporaryURL` and
`VariantURL` from loaded values without database or storage I/O. Authorize access
before creating links; script-capable files retain the download-only default.
Slots refuse implicit JSON serialization. Map loaded values into explicit DTOs.

## Writing slots

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go -->
```go
func saveArticle(ctx context.Context, tx *database.Tx, bound ext.Descriptors, article ext.Article, title map[i18n.LocaleID]string, seo string) error {
	if err := bound.Title.SyncIn(ctx, tx, article, title); err != nil {
		return err
	}
	return bound.SEO.SaveIn(ctx, tx, article, seo)
}
```

The `In` forms join the caller's transaction through savepoints. Other forms use
the manager's transaction. Parent rollback rolls back text and metadata writes.
Unknown commit outcomes require reconciliation, never an automatic retry.

`TextHandle.Save` merges locales; `Sync` makes supported locales match complete
input and enforces required locales. `Forget` removes one locale, and `Clear`
removes the field. `ValueHandle.Save` writes the declared metadata version and
contract; `Forget` removes it. Both have transaction-bound `In` forms.

Attachment handles provide `PrepareFile`/`Prepare`, `ReplaceFile`/`Replace`,
`ReplaceIn`, `Discard`, `Detach` and `Clear`; `ManyHandle` also has `AddFile`, `Add`,
`AddIn` and `Reorder`. Prepare uploads before the business transaction so its
snapshot can see them. Publish inside with `ReplaceIn` or `AddIn`; the owner may
have been created by that transaction. Failed publication leaves the stored upload
available for explicit recovery or `Discard`. Superseded files are cleaned after
commit. See [publication and recovery](attachments.md#publication-and-recovery)
for the full outcome contract.

`attachments.AddFilesTo(ctx, bound.Galleries, article, files)` reuses the bounded
batch implementation. It validates the bound handle and batch limit before opening
files, publishes in order, and returns every attempted result, including a failed
attempt. Earlier files remain published. Inspect unknown outcomes before retrying.

## Request input and validation

A `map[i18n.LocaleID]string` retains locale-keyed JSON and TypeScript contracts.
Use `value.Optional` for PATCH omission or a JSON multipart part. The articles
consumer binds its complete input to the handle's rule:

<!-- compiled-source: tests/fixtures/consumer/articles/http.go -->
```go
input := ArticleInputValidationFields()
	textRules := validation.All(
		input.Slug.Rules(validation.NonBlank[string]()),
		input.Author.Rules(validation.NonBlank[string]()),
		input.Title.Rules(s.x.Title.Rule()),
		input.Summary.Rules(s.x.Summary.Rule()),
	)
```

`Rule` enforces complete input for `SyncIn`. `MergeRule` validates partial input
for `SaveIn`, without requiring omitted locales. Both use one locale snapshot.
Unsupported keys report `foundry.supported_locale`, required empty or missing
locales report `foundry.required`, and oversized text reports `foundry.max_bytes`.
NUL text reports `foundry.not_matches`. Unbound rules fail registration.

`Accepts(ctx, file)` applies the write policy's name, size, detected media and
image checks without database or storage work. It buffers under manager admission.
Wrap it in a typed `validation.Custom` rule as the [HTTP example](../../tests/fixtures/consumer/articles/http.go)
does. The eventual write remains authoritative.

## Inspection and maintenance

`inspection.Sources{Models: app.Models()}` adds an `extensions` section to [declaration inspection](developer-tooling-and-testing.md#doctor-and-declaration-inspection):
each owner's persisted identity and every slot's field, kind, stored name,
storage table and policy bounds. `App.Models()` returns exactly the declarations registered with
`Builder.Models`, so inspection cannot drift from assembly. It reads declarations
only, so an agent or operator can see a model's extension storage without
starting services.

If the explicit stored-name literal changes, its rows remain under the old name. The read-only `undeclared` commands list stored names in an
owner's current scope that no registered slot or declaration uses:

```sh
translations undeclared --owner articles [--format text|json]
metadata undeclared --owner articles
attachments undeclared --owner articles
```

`translations.InspectUndeclared`, `metadata.InspectUndeclared` and
`attachments.InspectUndeclared` provide the same report. They read distinct names
with one query returning at most 256 names; PostgreSQL still scans the owner's
rows, so run them as maintenance. Beyond 256 stored names the report is marked
truncated (`(truncated)` in text output) and can omit undeclared names. They never
load stored text, values or files, and modify nothing. Restore the name
in the handle declaration, or remove the data deliberately through the explicit
APIs.

## Deletion cleanup

Each declaration registers a deletion observer (`orm.NewDeletionObserver`)
on every configured database connection. Its `Deleted` callback runs the existing
metadata, translation and attachment cleanup for every configured manager. Soft
deletion keeps the data for restoration; force deletion removes it. A deletion
observer takes part only in deletions: creates, updates, `UpdateAll` and
`Increment` keep their unhooked paths on every connection, while per-model
deletions take the observed path and set-based deletions (`DeleteAll`,
`ForceDeleteAll`), which would skip cleanup, still need `orm.WithoutModelHooks(query)`.

When the model is deleted through the extension store's connection, cleanup joins
the deletion transaction, so a parent rollback keeps the data and suppresses file
deletion. Through any other connection it cannot share that transaction: it runs
after the deletion commits, in a store transaction that first checks the owner is
still gone, so an owner deleted and created again keeps its data. A rolled-back
deletion schedules nothing. By default that cleanup is process-local after-commit
work: a failure is returned as the committed deletion's after-commit error, and a
crash before it runs leaves orphans for the inspection and pruning commands below.
The other connection must reach the model's table in the same database, as every
extension read and owner check already requires.

### Durable cleanup through the outbox

Set `features.extension_cleanup.jobs` to a job connection to make that cleanup
crash-safe, much as Laravel's database queue is when its `jobs` table shares the
transaction's connection, but on every connection:

```toml
[features.outbox]
enabled = true
jobs = { default = "extensions" }

[features.extension_cleanup]
jobs = "default"
```

Every configured connection then writes the outbox that the publisher relays, so
a deletion through another connection enqueues the `foundry.extensions.cleanup`
job (`slots.CleanupRequest`: owner name and persisted identity) inside its own
transaction. A rollback leaves no job, a commit keeps it, and a crash loses
nothing: the job waits in the outbox until a worker consuming the connection's
default queue runs it. The handler is idempotent. In a store transaction it skips
an owner that exists again and otherwise runs every configured manager's cleanup
(`CleanupIdentity`), which finds nothing left on a repeated delivery. Retries
follow the default job policy; an exhausted or permanently failed job appears in
[failed-job inspection](jobs-operations.md#inspect-and-explicitly-retry) and can be
retried there, and an unknown owner or malformed identity fails permanently.

Build refuses the setting unless `features.extensions` and `features.outbox` are
enabled and `features.outbox.jobs` has a destination for the connection, which
needs a durable job backend. Startup checks that every configured database
connection reaches the outbox's database and server, so a connection elsewhere
fails `Start` instead of writing rows nothing publishes. Deletions through the
extension store's own connection still clean up inside their transaction. Direct
assembly declares `slots.DefineCleanupJob(...).Declare(runtime)`, binds one
producer per pool with `ToOutbox`, and sets `Runtime.CleanupQueue` for the deletion
observers only, since descriptors never need it.

Remove any hand-written cleanup hook when adopting slots; a duplicate cleanup
after deletion is harmless. Bulk model deletes skip observers, so the existing
orphan inspection and pruning commands still apply.

### Explicit reads from ORM handles

Use `translations.FieldOf(slot)`, `metadata.KeyOf(slot)`,
`attachments.OneCollection(slot)` or `attachments.ManyCollection(slot)` when
an explicit batch read or collection operation is needed. Each function returns
the existing declaration, retaining its registration identity, owner, key and
value type. For example, `translations.FieldOf(bound.Title).Load(ctx, manager,
references)` reads the same field registered by `ArticleSlots.Title`.
The handles retain only shared callback state; the descriptor's larger API is
instantiated only by callers that request it. Zero handles return invalid zero
descriptors. Ordinary slot reads and writes continue to use the handle directly.

### Migrating a stored collection name

An old attachment slot's `foundry:"name=public-media"` tag becomes the explicit
`"public-media"` argument to `attachments.ManyOf` or `attachments.OneOf`. Remove
the tag: native `orm.SlotOf` binds a Go field, not a persisted column. Keep the
owner identity and `StorageModel` unchanged as well as the collection name. A
Go-field rename changes its binding, not that stored identity. See the compiled
[migration example](orm-migration.md#preserve-an-attachment-slots-stored-name).
