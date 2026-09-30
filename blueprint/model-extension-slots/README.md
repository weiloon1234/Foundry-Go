# Model extension slots

This continuation follows the design review requested on 2026-09-29. A model
declares translated text, file attachments and typed schemaless values as
ordinary Go fields, with their policy in one model method, similar to Laravel
model traits (`HasTranslations`, `HasAttachments`, JSON attributes) but checked
by the compiler and generator.

Milestone 18 already delivered the storage owners:
[translations](../../docs/guides/model-extensions.md#translated-model-fields),
[attachments](../../docs/guides/attachments.md) and
[typed metadata](../../docs/guides/model-extensions.md#typed-metadata) share
registered [extension owners](../../docs/guides/model-extensions.md#shared-owners)
and three framework tables. Today an application declares each field as a
package variable, registers it by hand, wires deletion cleanup itself and reads
values from separate batches (see the [profiles consumer](../../tests/fixtures/consumer/profiles/profile.go)).
This series adds the model-level declaration, generation, eager loading, write
and HTTP input layer over those owners. It creates no new storage table.

The [master](../00-master-architecture-and-parity.md#model-extension-slot-delivery)
owns status. This directory owns design and acceptance contracts. Read in order:

1. **E01** — [Slot declarations and generation](01-slot-declarations-and-generation.md)
2. **E02** — [Slot loading and reads](02-slot-loading-and-reads.md)
3. **E03** — [Slot writes and HTTP input](03-slot-writes-and-http-input.md)
4. **E04** — [Consumer fixture, tooling and documentation](04-consumer-tooling-and-documentation.md)
5. **E05** — [Integrated acceptance and final re-audit](05-acceptance-and-audit.md)

## Planned consumer shape

The snippets in this series are proposed contracts. Names and signatures are
finalized in consumer fixtures during their owning milestone.

```go
//foundry:model table=articles
type Article struct {
    ID   model.ID[Article]
    Slug string

    Title     translations.Text          // foundry_model_translations
    Summary   translations.Text
    Logo      attachments.One[Article]   // foundry_attachments and a disk
    Galleries attachments.Many[Article]
    SEO       metadata.Value[ArticleSEO] // foundry_model_metadata, no column

    Author relation.One[User]
}

func (Article) DefineExtensions() ArticleExtensionSet {
    return ArticleExtensionSet{
        Title: translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale},
        Logo: attachments.Policy{Disk: PublicFiles,
            Accepted: []storage.MediaType{"image/png", "image/jpeg", "image/webp"},
            Image:    value.Set(imaging.NewPlan().Fit(512, 512, false).Format(imaging.WebP))},
        Galleries: attachments.Policy{Disk: PublicFiles, MaxFiles: 20,
            Accepted: []storage.MediaType{"image/jpeg", "image/png"},
            Variants: []attachments.Variant{Thumbnail}},
    }
}
```

Assembly registers every declared model of a package in one line:

```go
app, err := application.New(settings).Models(models.FoundryExtensions()...).Build(ctx)
```

A handler binds the extension runtime once and then uses typed descriptors:

```go
type Articles struct {
    db *database.DB
    x  models.ArticleExtensionSlots
}

func NewArticles(db *database.DB, runtime slots.Runtime) Articles {
    return Articles{db: db, x: models.ArticleExtensions().From(runtime)}
}

list, err := models.QueryArticles().
    With(models.ArticleRelations().Author, h.x.Title, h.x.Logo).
    All(ctx, h.db)

err = h.db.Transaction(ctx, func(tx *database.Tx) error {
    a, err := models.QueryArticles().Create(ctx, tx, models.ArticleDraft{}.SetSlug(in.Slug))
    if err != nil {
        return err
    }
    if err := h.x.Title.SyncIn(ctx, tx, a, in.Title); err != nil {
        return err
    }
    article = a
    return h.x.SEO.SaveIn(ctx, tx, a, in.SEO)
})
if file, ok := in.Logo.Get(); ok {
    result, err = h.x.Logo.ReplaceFile(ctx, article, file)
}
```

## Decisions recorded on 2026-09-29

- **Slots, policy method and generation.** Reuse the relation pattern: typed
  struct fields declare what a model has, `DefineExtensions()` declares policy and
  `foundry generate` binds owner, registrations, descriptors and cleanup. Struct
  tags do not carry policy; policies contain typed disks, imaging plans and codecs.
- **Bind once.** Descriptors borrow managers through
  `ArticleExtensions().From(runtime)` in a constructor. No database-pool,
  context or process-global lookup supplies extension managers. The extension
  store may therefore use a different named connection than unrelated models.
- **Explicit mapping.** Request DTOs map to slots field by field
  (`h.x.Title.SyncIn(..., in.Title)`), as DTOs already map to drafts. There is no
  name-matched mass assignment. One call writes every configured locale of one
  translated field.
- **Stored names.** A slot's stored name is its snake_case Go field name unless
  `foundry:"name=..."` pins it. Renaming a Go field without the tag hides its
  stored data; E04 supplies read-only inspection of undeclared stored names.
- **Automatic cleanup.** Registering a generated declaration also registers its
  hard-delete cleanup observer. Soft deletion preserves extension data, as today.
- **Storage boundaries.** Translations and metadata join the caller's transaction.
  Attachments keep their recoverable publication workflow outside it.
- **Additive.** The explicit package-variable APIs, `profiles` consumer and
  application registration slices remain the advanced composition path.

## Existing contracts to preserve

- Owner identity: owner name plus storage model form the persisted scope; the
  [rename rules](../../docs/guides/model-extensions.md#owner-identity-and-table-renames),
  natural-key and bulk-delete rules apply unchanged to generated owners.
- Bounds, admission, cancellation and callback ownership of the extension store
  and each manager. Load batches stay within existing owner, row and byte limits.
- Translation semantics: canonical `i18n.LocaleID`, catalog membership, empty text
  as a present value, fallback order and retention of removed locales.
- Attachment publication, recovery, variants, active-content refusal, detected
  media and `Result.Publication`. No attachment value serializes implicitly.
- Metadata versioning and strict decoding; incompatible stored versions fail.
- Relation loading limits, loaded/empty/absent states and no hidden I/O on reads.
- Drafts contain only persisted columns. Slots are never draft fields.

## Scope and ownership

- Framework only, with independent consumer acceptance fixtures; no starter.
- Package layering stays acyclic: `extensions` ← `translations`, `attachments`,
  `metadata` ← a new binding package (working name `extensions/slots`) ←
  `application`. `database/query` imports none of them; it exposes one new
  externally fetched relation kind instead.
- No new wire schema. Localized input uses the existing `map[i18n.LocaleID]string`
  DTO support; uploads use `foundryhttp.UploadedFile`; metadata values use
  generated DTO descriptors. Persistence models do not become public DTOs.
- PostgreSQL only. Existing named/default services and application ownership
  remain the assembly contract.

## Implementation cadence

Finish each milestone's implementation, regression sources, consumer examples and
documentation before compiling/testing. Then run its consolidated verification
round, collect failures, batch fixes and repeat affected checks until the final
source passes its required gate. Reuse the existing compiler, generation and
editor batching.

Every implementation milestone requires `make verify`, relevant native PostgreSQL
integration and races, deterministic generation, public consumer acceptance and
the applicable compiler/TypeScript/gopls checks. Review consumer ergonomics before
proceeding. E05 additionally requires one complete re-audit of the series,
fixes and final-source verification.

This blueprint delivery itself uses documentation/link checks and source review
only. Implementation gates must not be reported as run merely because the plan
names them.
