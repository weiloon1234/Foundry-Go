# Model extension slots

A model can declare translated text, file attachments and typed schemaless
values as ordinary Go fields. The policy for all of them lives in one model
method, similar to Laravel model traits such as `HasTranslations` or
`HasAttachments`, but checked by the compiler and generator. The data is stored
in the shared framework tables of [model extensions](model-extensions.md) and
[attachments](attachments.md); a slot never adds a column or a migration to the
model's own table. The [articles consumer](../../tests/fixtures/consumer/articles/article.go)
is the compiling example.

The [model extension slot series](../../blueprint/model-extension-slots/README.md)
owns the design; the [master](../../blueprint/00-master-architecture-and-parity.md#model-extension-slot-delivery)
records delivery status.

## Declare slots on the model

```go
//foundry:model table=articles
type Article struct {
    ID        model.ID[Article]
    Slug      string
    DeletedAt value.Nullable[temporal.DateTime]

    Title     translations.Text          // foundry_model_translations
    Summary   translations.Text
    Logo      attachments.One[Article]   // foundry_attachments and a disk
    Galleries attachments.Many[Article]
    SEO       metadata.Value[SEO]        // foundry_model_metadata, no column
}

func (Article) DefineExtensions() ArticleExtensionSet {
    return ArticleExtensionSet{
        Title: translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale},
        Logo: attachments.Policy{Disk: Files, Accepted: []storage.MediaType{"image/png"},
            Image: value.Set(imaging.NewPlan().Fill(32, 32, false).Format(imaging.PNG))},
        Galleries: attachments.Policy{Disk: Files, MaxFiles: 4,
            Accepted: []storage.MediaType{"image/png"}, Variants: []attachments.Variant{Thumbnail}},
    }
}
```

| Field type | Stored in | Policy entry | Zero policy |
| --- | --- | --- | --- |
| `translations.Text` | `foundry_model_translations`, one row per locale | `translations.Options` | 64 KiB per value, no required locale |
| `attachments.One[M]` | `foundry_attachments` and the policy's disk | `attachments.Policy` | invalid: a Disk is required |
| `attachments.Many[M]` | the same, in collection order | `attachments.Policy` | invalid: a Disk is required |
| `metadata.Value[V]` | `foundry_model_metadata` | `metadata.Spec[V]` | version 1 and an inferred JSON contract |

Slots are not columns: generation excludes them from SQL, drafts, codecs and
audit. The `M` argument of `One`/`Many` must be the enclosing model. The slot
type selects cardinality, so a policy's `Cardinality` must be zero or match.
Localized attachment collections keep the explicit `ForLocale` API; a slot
policy with `Localized: true` is rejected.

`DefineExtensions` uses a value receiver and returns the generated
`<Model>ExtensionSet`. It is required when the model has an attachment slot and
optional otherwise; omitted entries use the zero policy. It runs once per
process and must not call `<Model>Extensions()`.

`translations.Options.Require` is an `i18n.LocaleRequirement` selecting the
locales translated input must supply with nonempty text: `i18n.OptionalLocales`
(the zero value), `i18n.DefaultLocale` or `i18n.AllLocales`. The slot's input
rule and exact synchronization enforce it; lower-level translation writes do not.

A metadata slot infers its JSON contract from a generated DTO (`SEOJSON()`), a
string, boolean, integer or floating value, a type with a `JSONContract` method,
or `json.RawMessage` through `contract.DynamicJSON()`. Enums are not inferred,
because a scalar contract would accept values outside their members; wrap an enum
in a DTO. Declare `metadata.Spec[V]{JSON: ...}` for any other value type; generation fails when
such a slot has no `DefineExtensions`, and assembly fails when the entry stays
empty. Adding optional fields to a DTO value needs no migration. Removing or
renaming a field requires a `Version` increment, because strict decoding rejects
stored unknown fields.

## Stored names and owner identity

A slot's stored name is its snake_case Go field name (`Galleries` →
`galleries`). Renaming the Go field changes the stored name and hides existing
data, like renaming a column without a migration. Pin the stored name before
renaming the field (the generated field notice states whether a name is pinned):

```go
Content translations.Text `foundry:"name=body"`
```

Stored names are lowercase letters, digits, `_`, `.` or `-`, and are unique
across the model's slots. `From` and `All` are reserved Go field names.

Generation declares one [extension owner](model-extensions.md#shared-owners) per
model with slots. Its owner name and storage model default to the table name,
which gives the same scope as `extensions.DefineOwner` with that name. A model
can therefore move from hand-written declarations to slots without moving data.
Before renaming a table, pin the owner with the model directive:

```go
//foundry:model table=posts extension_owner=articles
```

Recovery after an undeclared rename, with `PreviousModels` and the `rescope`
commands, stays on the explicit owner path.

## Generated API

For `Article`, generation emits:

- `ArticleExtensionSet`, the policy type returned by `DefineExtensions`;
- `ArticleExtensionSlots`, with one typed descriptor per slot:
  `translations.TextSlot[Article, K]`, `attachments.OneSlot[Article, K]`,
  `attachments.ManySlot[Article, K]` and `metadata.ValueSlot[Article, K, V]`;
- `ArticleExtensions()`, the unbound descriptors, built once per process, with
  `From(runtime)` to bind them and `All()` listing every slot for eager loading;
- `ArticleExtensionOwner()`, the owner shared with the explicit APIs;
- `ArticleExtensionDeclaration()` and a package-level `FoundryExtensions()`
  listing every slot-owning model of the package.

Each descriptor exposes its underlying declaration (`Field()`, `Collection()` or
`Key()`), so every existing translation, attachment and metadata operation keeps
working with the generated owner. Descriptors and slot fields carry generated
documentation, and `foundry generate --field-docs` adds a notice beside each slot
field naming its storage, policy and descriptor.

## Application assembly

Register a model package once:

```go
app, err := application.New(settings).
    Models(models.FoundryExtensions()...).
    Build(ctx)
```

Each declaration adds its owner and slots to the existing extension, translation,
attachment and metadata managers, so duplicate detection and limits stay in those
owners. `Build` fails before acquiring resources when a declaration needs a
disabled feature, naming its owner: every slot needs `features.extensions`,
translated slots need `features.locales`, and attachment slots need
`features.attachments` with storage.

Bind the runtime once in a constructor. `Services.ModelExtensions()` returns the
enabled managers:

```go
runtime, err := services.ModelExtensions()
x := models.ArticleExtensions().From(runtime)
```

Descriptors borrow the managers; they never look services up per call. A
manager left nil, for example in a hand-built `slots.Runtime`, leaves that slot
kind unbound. Direct foundation assembly registers a declaration with
`declaration.Register(registrar, poolKey, resolve)` and passes its `Owner()` and
`Parts()` to the managers it constructs.

## Loading slots

A bound descriptor is an ordinary eager-loading relation:

```go
list, err := models.QueryArticles().
    With(models.ArticleRelations().Author, x.Title, x.Logo, x.Galleries, x.SEO).
    All(ctx, tx)
```

Slots load wherever relations load: `All`, `First`, `Find`, `RequireFind`,
numbered, simple and cursor pagination, `Each`/`Chunk` per batch, explicit
`Load` and `LoadMissing`, and nested descriptors such as
`QueryAuthors().With(AuthorRelations().Articles.With(x.Title))`. `x.All()`
returns every slot for an edit screen: `With(x.All()...)`. `Count` and `Exists`
never load slots, and repeating a slot in one query fails validation. An
unbound descriptor fails validation with `fault.Missing` before the parent SQL.

Each slot costs its manager's batch queries per parent batch, independent of the
number of parents: an owner check and translation pages for text, an owner check,
the files and their variants for attachments, and an owner check and values for
metadata. Parent batches follow `RelationLimits.BatchSize` (500 by default), and
attachment batches are further limited so `MaxFiles` fits the 4096-file bound. A
batch beyond a store's row or byte limit is halved and retried, so large content
loads whenever each owner fits alone.
Each loaded value (a stored text set, metadata value or file) is charged against
the query's `RelationLimits` budget as fetched and attached, per parent batch, so
an over-budget read stops early. Slots also respect `MaxDepth`.

Loading two translated fields and one metadata value through `With` costs about
the same as the equivalent explicit batch loads: about 1.9 ms for one article
(0.2 ms more than explicit loads), 17–26 ms for 100 and 160–170 ms for 1000
(within 5%), against 0.3–1.6 ms for the parent rows alone, on native PostgreSQL
on an Apple M4 Max ([measurements](../evidence/model-extension-slots-performance-20260930.json);
the [E05 baseline](../evidence/model-extension-slots-e05.json) was 280 ms for
1000). Most of the remaining cost is decoding and checking each stored row's
persisted owner identity, shared by both paths; load only the slots a response
needs, such as `x.Title` and `x.Logo` for a listing.

When the parent query runs in a `*database.Tx` of the extension store's pool,
slot loading joins that transaction through a savepoint and sees its own
uncommitted writes. Any other executor, including a custom wrapper, uses the
store's own read-only snapshot. The parent query and slot loads are separate
statements, as with relations. A parent that is soft-deleted (for example read
with `WithTrashed`) or deleted concurrently keeps an unloaded slot, because
extension data of inactive owners is unavailable to ordinary reads.

## Reading loaded slots

Reads never perform I/O. Reading an unloaded slot reports `fault.Missing` or
its loaded flag; a loaded slot distinguishes empty from present.

| Slot | Reads |
| --- | --- |
| `translations.Text` | `Exact(locale)`, `Resolve(locale)`, `ResolveRequest(ctx)` and `Values()`, with the existing fallback: requested locale, supported regional parents, catalog default, then the remaining supported locales. Empty text is a present value. `ResolveRequest` uses `i18n.RequestLocale`, or the catalog default. |
| `attachments.One[M]` | `Get() (value.Optional[attachments.File[M]], bool)` |
| `attachments.Many[M]` | `Get()`, `Len()` and `All()` in collection order |
| `metadata.Value[V]` | `Get() (value.Optional[V], bool)`, decoded once at load |

`attachments.File` carries the file's ID, collection, detected info, position,
properties and ready variants. Bound attachment descriptors derive links from
loaded slots without database or storage I/O: `x.Logo.PublicURL(ctx, article)`
returns an absent optional for an empty slot, `x.Galleries.PublicURLs(ctx,
article)` returns links in collection order, and `URL`, `TemporaryURL` and
`VariantURL` link one loaded file. They apply the collection's rules, including
the download-only default for script-capable files and `VariantUnavailable`.
Authorize access before creating links. Slots refuse implicit JSON serialization;
map loaded values into response DTOs.

## Writing slots

Bound descriptors write for a model value; they derive its owner reference
without I/O. The `In` forms join a caller's transaction through a savepoint, so a
parent rollback rolls the write back; the other forms use the manager's own
transaction. No write is retried automatically after an uncertain commit.

```go
err = db.Transaction(ctx, func(tx *database.Tx) error {
    article, err := models.QueryArticles().Create(ctx, tx, models.ArticleDraft{}.SetSlug(in.Slug))
    if err != nil {
        return err
    }
    if err := x.Title.SyncIn(ctx, tx, article, in.Title); err != nil {
        return err
    }
    return x.SEO.SaveIn(ctx, tx, article, in.SEO)
})
```

| Slot | Writes |
| --- | --- |
| `TextSlot` | `SaveIn`/`Save` merge the supplied locales in one statement and keep the others. `SyncIn`/`Sync` make the supported locales exactly the input and enforce `Options.Require`; locales removed from the catalog keep their retained rows. `ForgetIn`/`Forget` remove one locale and `ClearIn`/`Clear` the field. Every entry is validated before writing; empty text is a present value. |
| `ValueSlot` | `SaveIn`/`Save` store the value with the declared version and contract; `ForgetIn`/`Forget` remove it. |
| `OneSlot`, `ManySlot` | `ReplaceFile`, `Replace`, `Detach` and `Clear`; `ManySlot` adds `AddFile`, `Add` and `Reorder`. |

Attachments keep their recoverable publication workflow outside the caller's
transaction: commit the model, its text and metadata first, then publish files
and inspect `Result.Publication` when an error is returned. `ReplaceFile` and
`AddFile` accept any `attachments.FileSource`, such as `foundryhttp.UploadedFile`,
and open and close it within the call; request cleanup still owns the spooled
file. The client content type is only the existing text hint, with parameters
dropped. `attachments.AddFiles(ctx, x.Galleries, article, files)` publishes files
in order and stops at the first failure, returning every attempted result;
files before a failure stay published.

## Request input and validation

Translated input needs no new DTO type. A `map[i18n.LocaleID]string` field is a
JSON object keyed by locale, and a multipart form carries it as a JSON part
(`form:",json"`); use `value.Optional` of the map for PATCH omission. Validation
comes from the slot:

```go
fields := ArticleInputValidationFields()
endpoint.WithBodyValidation(fields.Title.Rules(x.Title.Rule()))
```

`Rule()` validates complete input written with `SyncIn`. It takes one locale
catalog snapshot per check and reports issues at entry paths: an unsupported key
as `foundry.supported_locale` (`/body/title/fr`), a missing or empty required
locale as `foundry.required` (`/body/title/ms`) and oversized text as
`foundry.max_bytes`. Values containing NUL, which writes reject, fail as
`foundry.not_matches`; a locale key that cannot be a path segment is reported
at the map itself. `MergeRule()` validates partial input written with `SaveIn`
and omits the required-locale check, because a merge keeps unmentioned locales. It is built from the new server-only
`validation.Locales` and `validation.MaxBytes` rules, which other locale-keyed
inputs can use directly. An unbound slot's rule fails endpoint registration.

`Accepts(ctx, file)` checks an upload against the slot's policy with the same
filename, size and byte-detected media checks as a write, including image
inspection, and without storage or database work. It buffers the upload under
the manager's write admission, like a write. It returns false for a
rejected file. Wrap it in `validation.Custom` with an application rule ID:

```go
logo := validation.Custom(validation.Spec{ID: "articles.logo", Message: "The logo is not an accepted image."},
    func(ctx context.Context, file foundryhttp.UploadedFile) (bool, error) { return x.Logo.Accepts(ctx, file) })
```

HTTP's generic content sniff is not a substitute: it reports OOXML documents as
ZIP archives that attachments accept. The write stays authoritative, including
image transformation limits. The [articles consumer](../../tests/fixtures/consumer/articles/http.go)
exercises JSON and multipart endpoints end to end.

## Inspection and maintenance

`inspection.Sources{Models: app.Models()}` adds an `extensions` section to [declaration inspection](developer-tooling-and-testing.md#doctor-and-declaration-inspection):
each owner's persisted identity and every slot's field, kind, stored name,
storage table and policy bounds. `App.Models()` returns exactly the declarations registered with
`Builder.Models`, so inspection cannot drift from assembly. It reads declarations
only, so an agent or operator can see a model's extension storage without
starting services.

After a slot field is renamed without `foundry:"name=..."`, its rows remain under
the old stored name. The read-only `undeclared` commands list stored names in an
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
with `foundry:"name=..."`, or remove the data deliberately through the explicit
APIs.

## Deletion cleanup

Each declaration registers a hard-delete observer on the extension store's
database. Its `Deleted` callback runs the existing metadata, translation and
attachment cleanup for every configured manager inside the deletion transaction,
so a parent rollback keeps the data and suppresses file deletion. Soft deletion
keeps the data for restoration; force deletion removes it. Models with slots must
be written through the extension store's database, because cleanup joins the
owner's transaction.

Remove any hand-written cleanup hook when adopting slots; a duplicate cleanup
after deletion is harmless. Bulk model deletes skip observers, so the existing
orphan inspection and pruning commands still apply.
