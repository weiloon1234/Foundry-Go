# Declare a model once

<a id="agent-entry"></a>
## Agent entry

- Declare: [Model and generated table](#recipe-declare-table)
- Choose: [Fields and roles](#recipe-field-roles)
- Delete: [Soft-delete capability](#recipe-soft-delete)
- Register: [Tables and identity](#recipe-register-tables)
- Regenerate: [Stale declarations](#recipe-stale-declarations)
- Transform: [Mutators](model-mutators.md#recipe-mutate-field)
- Observe: [Hooks](model-hooks.md#recipe-write-hooks)

<a id="recipe-declare-table"></a>
## Declare a model and generate its table

Write a defined, nongeneric struct in a non-test Go file. Attach
`//foundry:model table=users var=Users key=ID` to its type documentation. The three
required options name the SQL table, generated table variable and persisted key
field. Directive options are separated by spaces; `foundry` tag options use commas.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `attachments`, `database`, `database/orm`, `decimal`, `fault`, `model`, `translations`, `value`. A complete file block shows its own import list; the guide's blocks that import `models` use the application's own package of declared models.

This complete fixture file declares two plain models:

<!-- compiled-source: tests/fixtures/consumer/bootstrap/models.go -->
```go
package bootstrap

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=bootstrap_members var=Members key=ID identity=false
type Member struct {
	ID   model.ID[Member] `foundry:"member=ID"`
	Name string
}

//foundry:model table=bootstrap_operators var=Operators key=ID identity=false
type Operator struct {
	ID         model.ID[Operator] `foundry:"member=ID"`
	Department string
}
```

Run the module-pinned tool from the application root:

```sh
GOWORK=off go tool foundry generate --recursive
GOWORK=off go tool foundry generate --check --recursive
GOWORK=off go tool foundry models check ./...
```

For `user.go`, generation writes `user_foundry.gen.go` beside it and records
ownership in `.foundry-gen.json`. It emits `UserTable`, `Users`, the typed field
bindings and, by default, `usersIdentity`, `User.FoundryReference` and
`User.FoundryIdentity`. The fixture above opts out of identity with `identity=false`.
Keep generated output and its manifest with the source. Never edit either by hand.

Table members follow struct order. The embedded `Table` is first; a soft-delete
capability named `Soft` is last. Keys are available through `Users.Key()` unless
explicitly exposed as members. Identity names lowercase the first word of the
table variable: `Users` becomes `usersIdentity`; `APIKeys` becomes `apiKeysIdentity`.
Handwritten extension declarations in the same package can reuse that variable.

Generation creates no SQL tables, migrations, query wrappers, drafts or response
DTOs. Keep schema changes in migrations, queries in ordinary typed ORM calls and
public responses in separate DTOs. Row projections remain handwritten.

<a id="recipe-field-roles"></a>
## Fields and roles

Fields infer their normal typed handles. Add a tag only to choose different
behavior; redundant options fail generation.

| Struct field or option | Generated behavior |
| --- | --- |
| String, integer or float | Text or number handle, retaining named types. |
| Boolean, bytes or `model.ID[Other]` | Boolean, bytes or ID codec with a column handle. A foreign ID does not create a foreign-key constraint. A `model.ID[T]` is stored in a PostgreSQL `uuid` column, so a migration declares it as `uuid`. |
| Generated enum | Enum handle and its descriptor's codec. Do not add `role=enum`. Declare the enum with `//foundry:enum` as shown in [database values](database-codecs.md#binding-scanning-and-validation). |
| `value.Nullable[T]` | Nullable handle for the stored family. NULL remains distinct from a supplied zero. |
| `decimal.Decimal` | Choose `role=number` for numeric operations or `role=column` for a plain column. |
| `time.Time`, `temporal.DateTime`, `Date`, `Time`, `LocalDateTime`, `Interval` | Ordinary time or interval handle unless a managed role is explicit. See conventional-name rules below. |
| `password.Hash`, encrypted text or JSON | The required sensitive codec or encrypted envelope constructor. |
| `value.JSON[T]` | JSON handle using the payload contract. |
| `column=email_address` | Overrides the inferred snake-case column name. A redundant override is rejected. |
| `default=database` | Omitted insert values use the database default; the migration must define it. |
| `input=EmailInput` | Write assignments accept `EmailInput`; reads and predicates retain the stored type. |
| `mutate=normalizeEmail` | Names a package-local `func(EmailInput) (Stored, error)` instead of the conventional `MutateEmail` method. Requires `input`. |
| `codec=emailCodec` | Calls a package-local `func() codec.Codec[Stored]` and selects a plain column. Do not combine it with a role. |
| `role=column` | Selects a plain column when that differs from the inferred constructor. |
| `values=Draft\|Published` | Declares the enum cases a deliberately narrower column holds, by Go constant name. Only on an enum field or a collection of enums; run `foundry generate` after adding it. See [enum columns and their lists](#recipe-enum-narrowing). |
| `foundry:"-"` | Excludes a transient field from persistence; it still participates in the structural guard. |

`value.Optional` is a write or transport state, not a persisted column. Embedded
fields, including ignored embedded fields, are unsupported. Persisted fields must
be exported. Custom stored types require a codec factory. Encrypted fields cannot
use custom column codecs, database defaults or input mutators, and require an
application-assigned primary key. Password input requires an explicit password
codec and an input type.

The following complete file shows nullable decimals, their explicit role and a
model-owned key:

<!-- compiled-source: tests/fixtures/consumer/models/ledger.go -->
```go
package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=ledger_entries var=LedgerEntries key=ID identity=false
type LedgerEntry struct {
	ID model.ID[LedgerEntry] `foundry:"member=ID,key-role=primary"`
	// Amount selects exact stored decimals. Example: orm.Eq(LedgerEntries.Amount, amount).
	Amount  decimal.Decimal                 `foundry:"role=number"`
	Balance value.Nullable[decimal.Decimal] `foundry:"role=number"`
}
```

This complete declaration combines a nullable decimal, an enum, a foreign ID and
a managed timestamp pair. The foreign ID retains its model owner; migrations
still define its constraint.

<!-- compiled-source: tests/fixtures/goway/modelguide/product.go -->
```go
// Package modelguide supplies complete generated declarations for the guides.
package modelguide

import (
	"time"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=guide_products var=Products key=ID
type Product struct {
	ID        model.ID[Product]
	OwnerID   model.ID[models.User]
	Status    models.Status
	Price     decimal.Decimal                 `foundry:"role=number"`
	Discount  value.Nullable[decimal.Decimal] `foundry:"role=number"`
	CreatedAt time.Time                       `foundry:"role=created-at"`
	UpdatedAt time.Time                       `foundry:"role=updated-at"`
}
```

### Timestamp choices

Use `role=created-at` and `role=updated-at` together, once each, on non-key,
nonnullable `time.Time` or `temporal.DateTime` fields. Use `role=soft-delete` on a
non-key nullable instant. Managed timestamps cannot use database defaults or input
mutators. Explicit managed roles can use field names other than the conventional names.

A lone ordinary time-family field named `CreatedAt` or `UpdatedAt` needs
`role=time` while conventions are enabled. A string or interval with such a lone
name keeps its inferred role. An active `CreatedAt`/`UpdatedAt` pair instead needs both managed roles,
or `timestamps=none` on the directive with timestamp role tags removed. An eligible active
`DeletedAt` needs `role=soft-delete`, or `softdeletes=none` with its role tag removed.
If the type, input or database-default setting makes a managed role invalid, the
generator offers only the model-level opt-out.
`role=time` does not disable conventions. It is redundant on unrelated names or
when that convention is disabled. Explicit managed roles take precedence over
conventional names; ignored fields and slots do not participate. A custom codec or
explicit plain-column role on a conventional field retains the ORM's existing
convention opt-out. Choose those only when a plain column is the intended API.

<a id="recipe-enum-narrowing"></a>
### Enum columns and their lists

When an enum column carries a database `CHECK` constraint, `foundry models check`
evaluates it for every case of the enum and requires that none is rejected (see
[keep an enum and its database list equal](migrations-and-seeding.md#recipe-enum-lists)).
A column that deliberately holds fewer cases says so on the field:
``Stage Status `foundry:"values=Draft|Published"` ``. The names are the Go constants of
the enum's cases, separated by `|`. The option is part of the model's declaration, so run
`foundry generate` after adding it; the generated-output check fails until you do. Then
the constraints must reject every case the tag leaves out and admit the named ones, a new
enum case leaves the column alone, and the generator refuses `values` on a field that
holds no enum (a field of an enum, `value.Nullable`, `value.JSON`, slice or array of one
qualifies). `models check` refuses a name that is not a case and a list that names every
case.

### Key capabilities and member names

The key must be persisted, exported, nonnullable and comparable. A `model.ID[M]`
primary key must belong to `M`. Integer and string keys, including named types and
enums, have default codecs. Other supported comparable keys need `codec=name`.

`member=ID` exposes a key named `ID`. Exposed numeric and string keys infer lookup
capabilities; an ordinary key remains available through `Table.Key()`.
`key-role=primary`, `primary-number` or `primary-text` enables insert-select mapping.
`key-role=input` requires an exposed member and `input=Type`, and cannot also grant
primary insert-select mapping. `lookup`, `number` and `text` select nondefault key
capabilities when applicable; redundant choices are rejected. Input keys with a
distinct input type do not synthesize a UUID automatically.

For non-key fields, `member=none` keeps persistence but hides the handle. Rename
an exposed member with `member=Name` only when its field name collides with a
promoted table method, such as `Key` or `Query`, or the generated embedded field
`Table`. There is no general rename,
table-type-name or member-order option.

### Hooks, mutators and slots

A directive can name package-local, nongeneric factories:

| Option | Required signature |
| --- | --- |
| `hooks=memberHooks` | `func() orm.Hooks[Member]` |
| `retrieval=memberRetrieval` | `func() orm.RetrievalHooks[Member]` |
| `scopes=memberScopes` | `func(MemberTable) []orm.GlobalScope[Member]` |

This complete declaration combines soft deletion, a hook, a distinct mutation
input, translation and attachment slots. The [owner and policies](model-extensions.md#shared-owners)
use its generated identity and retain explicit stored slot names.

<!-- compiled-source: tests/fixtures/goway/modelguide/article.go -->
```go
package modelguide

import (
	"context"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

type SlugInput struct{ Text string }

//foundry:model table=guide_articles var=Articles key=ID hooks=articleHooks
type Article struct {
	ID        model.ID[Article]
	Slug      string `foundry:"input=SlugInput"`
	Revision  int
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
	Title     translations.Text
	Cover     attachments.One[Article]
}

func (Article) MutateSlug(input SlugInput) (string, error) {
	return strings.ToLower(strings.TrimSpace(input.Text)), nil
}

func articleHooks() orm.Hooks[Article] {
	return orm.Hooks[Article]{Deleting: func(_ context.Context, _ *database.Tx, article Article) error {
		if article.Slug == "protected" {
			return fault.Conflict
		}
		return nil
	}}
}
```

`input=Type` is required even for a same-type mutator. A value-receiver
`Mutate<Field>(Input) (Stored, error)` supplies the conventional transform; otherwise
name a function with `mutate=name`. Do not declare both. Accessors remain explicit
business methods; they do not change persistence or identity.

`relation.One`, `relation.Many`, `relation.Through` and `relation.Value` fields infer relation slots.
`attachments.One/Many[Model]`, `translations.Text` and `metadata.Value[T]` infer
external-store slots. Attachment owners must match the declaring model. Use bare
`slot` only for an external-store type that is not recognized. A slot takes no
column, default, role, input or codec option. Persisted slot names and policies
stay in handwritten extension declarations, never in the tag. See the complete
[Article with soft deletion and slots](../../tests/fixtures/consumer/articles/article.go)
and [extension setup](model-extension-slots.md).

<a id="recipe-soft-delete"></a>
## Use the soft-delete capability

`role=soft-delete` generates `Users.Soft`. Force deletion and bounded restoration
require that exact declaration's token. Wrong-model or missing tokens fail
compilation; zero or foreign-declaration tokens fail validation before SQL. The
token grants neither visibility nor authorization. Use `WithTrashed` or
`OnlyTrashed` explicitly when selecting deleted rows.

See [model writes](model-writes.md) for operations and [soft-delete fixture](../../tests/fixtures/consumer/softqueries/models.go)
for a complete declaration with timestamps, a hook and a mutator.

## SQL assignment constraints

`rows.MapInsert` requires `orm.InsertableRole`; `rows.MapUpdate` requires
`orm.UpdatableRole`. Input fields and managed update timestamps satisfy neither.
Primary insertion requires an exposed key with a primary key role; a lookup key
cannot acquire that permission by conversion. Primary keys never satisfy update.
Managed creation time remains eligible for insert-select. These roles do not
change the stored schema or response contracts.

<a id="recipe-register-tables"></a>
## Register tables and use identity

Pass the complete table list to `application.WithTables` before resources start.
For a direct declaration check, use the same list with `orm.Check`. `Tables()` is the application's function returning the generated tables of all its models. The statements run in a function returning `error`:

<!-- compiled-source: tests/fixtures/consumer/productionprofile/queries.go#check-table-registry -->
```go
if err := orm.Check(Tables()...); err != nil {
	return err
}
```

Neither check discovers an omitted model. Query execution also rejects invalid
declarations. The generator does not emit a package-wide `Tables()` registry.

Use `user.FoundryReference()` for a typed reference and `user.FoundryIdentity()`
for its serialized identity. The generated methods read the stored key through
its codec, without an accessor or database read. `identity=false` omits the
identity variable and both methods; use it only when those methods are not needed
or an existing custom identity contract must remain. `identity=true` is redundant.

<a id="recipe-stale-declarations"></a>
## Stale declarations

| Source change without regeneration | Detection |
| --- | --- |
| Add, remove, rename, reorder or retype a field, including an ignored field | Compilation fails at the generated shape conversion, beside `Struct changed: run foundry generate.` |
| Change, add or remove a `foundry` tag | The declaration fingerprint fails `orm.Check`, application table validation or query validation with `model changed since generation; run foundry generate`. |
| Change the directive comment, such as `table`, `key` or `hooks` | `foundry generate --check` detects stale output. Comments are invisible to the runtime fingerprint. |
| Change another tag namespace | It is outside the model fingerprint. Its owning generator or runtime validates that contract. |

The fingerprint covers ordered field names and exact `foundry` tag presence and
values, including ignored fields. The ORM recomputes it once at declaration time;
there is no query-path hashing. `generate --check` covers generated output and
ownership in addition to these two guards. It prints one `[generated-stale]` line per
stale file and a closing `scope:` line: run `foundry generate` with the same `--dir`
and `--recursive` that the check used. Run it in the gate alongside
`foundry models check`; the latter checks declaration spelling, not freshness.

<a id="diagnose-declarations"></a>
## Diagnose a wrong declaration

`foundry generate` prints each declaration error as
`file:line:column: [generate-declaration] model Struct.Field: problem; correction. Foundry guide: model-declarations#diagnose-declarations`;
table-level errors name the struct without a field. The messages below are the actual
problem and correction text; angle-bracket names stand for the offending field, type or
option. A message not listed has the same shape: what is wrong, then what to change.
`foundry models check` prints a handwritten table as
`file:line:column: [model-table] model Struct: table is declared by hand; put the //foundry:model directive on the struct, run foundry generate, and delete the handwritten table`.

| Mistake | Diagnostic and correction |
| --- | --- |
| Handwrite a table in a checked non-test package | `[model-table]` from `foundry models check`: put the `//foundry:model` directive on the struct, run `foundry generate`, and delete the handwritten table |
| Misspell or space the directive prefix | `invalid Foundry directive spelling; use //foundry:model` |
| Misspell the tag namespace | `unknown struct tag foudry; use foundry` |
| Use a generic model directive | `generic models are outside the directive; build the table in a generic function using the low-level ORM functions` |
| Omit the table or key | `table requires an explicit SQL identifier; write table=<name> or table=<schema>.<name>`; `key requires a struct field name; write key=<Field> naming the primary-key field` |
| Guess a directive option | `unknown directive option <name>; the options are table, var, key, identity, timestamps, softdeletes, hooks, retrieval and scopes`; `type`, `table-type`, `identity-var`, `members` and `soft` are not options. |
| Guess a tag option | `unknown foundry option <name>; the options are column, default, role, input, mutate, codec, member, key-role, values and slot` |
| Repeat an inferred role | `role=enum is inferred from the field type; remove it` |
| Name a case that does not exist in `values` | `[enum-list]` from `foundry models check`: `values names Missing, which is not a case of enum Phase` |
| Name every case in `values` | `[enum-list]` from `foundry models check`: `values names every case of enum Phase; remove the values option` |
| Put `values` on a field that holds no enum | `values applies only to an enum field or a collection of enums; mark the field's type with //foundry:enum, or remove the values option` |
| Omit a decimal role | `decimal field requires role=column or role=number; declare one of them` |
| Omit managed roles on an eligible conventional pair | `CreatedAt and UpdatedAt are conventional timestamp names; declare role=created-at on CreatedAt and role=updated-at on UpdatedAt, or use timestamps=none on the model and remove timestamp role tags` |
| Omit an eligible conventional deletion role | `DeletedAt is a conventional deletion name; declare role=soft-delete on DeletedAt, or use softdeletes=none on the model and remove its role tag` |
| Omit input metadata or its transform | `Mutate<Field> requires input; declare input=<type> on the field or remove the method`; `input requires Mutate<Field> or mutate; declare the method or mutate=<function>` |
| Use a pointer receiver for field behavior | `model field method <Name> requires a value receiver; declare it on the value type` |
| Guess an external stored-name tag | `slot is a bare flag, not slot=<name>; write slot` |
| Add `slot` to a known slot family | `slot is inferred from the field type; remove it` |
| Embed a struct | `embedded fields are not supported; declare each model field directly` |
| Use a transport presence wrapper | `Optional is a write/transport state, not a persisted column; declare the plain type, or value.Nullable[T] for a nullable column` |
| Shadow a table method | `field collides with a promoted Table method; declare member=<Name> using a non-colliding name` |
| Handwrite a name the generator also emits | `<Name> is declared by handwritten code, but the Foundry declaration at <file:line:column> generates it in <file>; rename the handwritten declaration or the Foundry declaration`, at the handwritten declaration |
| Put the directive in a test file | `model declarations in _test.go files are not supported; move the model into a non-test Go file` |

An ineligible conventional timestamp offers only the model-level opt-out; it does
not suggest an impossible managed role. See [timestamp rules](model-timestamps.md).

## Low-level declaration boundary

The tag parser rejects names one edit or one adjacent transposition from `foundry`.
Namespaces beginning `foundry_`, `foundry-` or `foundry.` with a nonempty suffix are
reserved. Unrelated tags such as `found` and `boundary` remain valid.
Without whitespace after `//`, a near-miss namespace or separator followed by a
known directive kind is rejected regardless of the remaining text. With whitespace
after `//`, it is rejected only when the kind has no following text or only
`key=value` options; ordinary prose is left alone. The correction names the exact
directive, without echoing the typo or its options. Exact `//foundry:` prefixes
retain the unknown-kind and malformed-option errors.

Write field documentation on the model struct. Generation copies leading and
trailing field comments onto the generated table member, including the handles
used in queries, `orm.Set` and `orm.ChangeOf`. Each member links back to
`Model.Field`. Go-to-definition on a table handle lands on that generated member;
follow its model-field link to edit the handwritten declaration. Direct model-field
access resolves to the handwritten field itself. Use `foundry generate --check` to
detect stale directive comments and field documentation. The compile-time guard and
runtime fingerprint do not inspect comments.

Application models use the directive. `foundry models check` reports handwritten
table construction for concrete models with constant table names in non-test files
outside `database/orm`. Folded constant names are included. The check follows one
helper level across all packages selected by the run (`./...` from the module root
selects the whole module): when a function or method passes its own parameter directly to
`orm.Define` as the table name, the corresponding call argument decides whether
the name is constant. Direct returns and uniquely assigned returned locals identify
the relevant table when a helper declares several tables. If the returned table
cannot be resolved, any constant candidate name still causes a report. Other
string arguments do not affect that decision. Helpers outside the loaded packages,
including third-party modules, are not analysed; neither are deeper helper chains
or table-name expressions inside helpers. There is no exemption flag, comment or
list. Generic models and runtime-computed table names remain
outside the directive. Tests and the ORM tree can use the functions that generated
code calls to exercise shapes the generator cannot emit. See the
[generated-code reference](../../database/orm/README.md#generated-declaration-functions).
