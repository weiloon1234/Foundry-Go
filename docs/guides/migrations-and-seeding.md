# Migrations and seeding

<a id="agent-entry"></a>
## Agent entry

- [Declare immutable migrations](#recipe-declare-migration)
- [Keep an enum and its database list equal](#recipe-enum-lists)
- [Register every migration](#recipe-register-migration)
- [Register and run migration commands](#recipe-migration-targets)
- [Inspect and explicitly run migrations](#recipe-run-migrations)
- [Seed reference data](#recipe-seed-data)
- [Create concurrent indexes and reconcile](#recipe-online-migration)

## Overview

Foundry provides immutable migration definitions, history inspection, an explicit PostgreSQL runner, transactional seeders, consumer command adapters, and development scaffolding. Real PostgreSQL behavior is exercised through the [PostgreSQL adapter](postgresql.md).

<a id="recipe-declare-migration"></a>
## Historical definitions

Handwritten Go models describe current application types. A `migrate.Definition` records a historical schema change: a typed origin and ID, the release that introduced it, ordered SQL statements, and typed prerequisites. The [consumer migration package](../../tests/fixtures/consumer/migrations/migrations.go) and its [test](../../tests/fixtures/consumer/migrations_test.go) demonstrate public usage.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `application`, `database`, `database/command` as `dbcommand`, `infrastructure`, `database/migrate`, `model`, `database/orm`, `database/seed`, `value`.

The application declares its origin, migration IDs and introducing version once as typed constants:

<!-- compiled-source: tests/fixtures/consumer/migrations/migrations.go#migration-names -->
```go
const (
	Origin         migrate.Origin  = "consumer"
	CreateRecords  migrate.ID      = "20260911000000_create_records"
	AddRecordLabel migrate.ID      = "20260911000001_add_record_label"
	Introduced     migrate.Version = "v0.1.0"
)
```

`Definitions` returns the historical definitions, the second depending on the first:

<!-- compiled-source: tests/fixtures/consumer/migrations/migrations.go#migration-definitions -->
```go
func Definitions() []migrate.Definition {
	initial := migrate.Key{Origin: Origin, ID: CreateRecords}
	return []migrate.Definition{
		{Key: initial, Version: Introduced, SQL: []string{"CREATE TABLE consumer_records (id bigint PRIMARY KEY)"}},
		{Key: migrate.Key{Origin: Origin, ID: AddRecordLabel}, Version: Introduced, Requires: []migrate.Key{initial}, SQL: []string{"ALTER TABLE consumer_records ADD COLUMN label text NOT NULL DEFAULT ''"}},
	}
}
```

Use imports from `github.com/weiloon1234/Foundry-Go/database/migrate`. Semantic names use lowercase letters, digits, underscores, hyphens, and dots; they start with a letter or digit and contain at most 128 bytes. Define constants once and reuse them. Origins separate framework, plugin, and application history: framework packages use `foundry.*`, `plugin.MigrationOrigin` gives each plugin `plugin.<id>`, and applications use `app` or `app.<area>`. An applied origin is permanent and each database belongs to one application, so keep product names out of origins; renaming or copying an application then never changes its migration keys or checksums. The introducing version is immutable metadata, not the currently installed release version.

`migrate.New` rejects duplicate identities, invalid SQL text, missing dependencies, repeated/self dependencies, and cycles before database I/O. Independent definitions sort by ID then origin; declared dependencies execute first. Input slices and returned metadata are copied. Code that retains definitions before calling `migrate.New`, such as a plugin contribution, snapshots them with `Definition.Clone`, which copies `SQL`, `Down` and `Requires`. The SHA-256 checksum covers versioned canonical metadata, SQL in execution order, and sorted prerequisites. Even SQL whitespace changes the checksum. Keep applied definitions unchanged and add a new migration for a later change.

By default, each statement must be valid inside a PostgreSQL transaction. Use separate SQL entries for separate statements. Do not include `BEGIN`, `COMMIT`, `ROLLBACK`, or manual savepoint commands, and use explicit `Mode: migrate.NonTransactional` for operations requiring execution outside a transaction. Foundry owns the transaction and its history insertion. SQL content is an explicit reviewed boundary; Go types cannot prove SQL semantics.

<a id="recipe-enum-lists"></a>
## Keep an enum and its database list equal

A column that holds a `//foundry:enum` often carries a `CHECK` constraint that lists
the enum's values in SQL: `CHECK (status IN ('draft','published'))`, or
`CHECK (roles <@ '["reader","editor"]'::jsonb)` for a JSON array of cases. The Go enum is
the source of truth; the migration holds a copy that nothing at compile time ties to it.
An enum case added without a migration compiles and passes every check that needs no
database, and the database then rejects the value at run time.

`foundry models check` closes that gap without a database. For every model field whose
type is an enum, or a `value.Nullable`, `value.JSON`, slice or array of one, it finds the
CHECK constraints that are live on that table and column after the last migration (the
migrations are replayed in the registry's order: prerequisites first, then by ID) and
evaluates each one once per enum case, requiring that none rejects a case. The fixture's
declarations:

<!-- compiled-source: tests/fixtures/consumer/enumlists/documents.go#enum-list-declarations -->
```go
//foundry:enum
type Status string

const (
	Draft     Status = "draft"
	Published Status = "published"
	Archived  Status = "archived"
)

//foundry:enum
type Role string

const (
	Reader Role = "reader"
	Editor Role = "editor"
)

//foundry:model table=enum_list_documents var=Documents key=ID
type Document struct {
	ID    model.ID[Document]
	Title string
	// Status holds every Status case; its constraint lists them all.
	Status Status
	// Roles holds a JSON array of Role cases; its constraint lists them all.
	Roles value.JSON[[]Role] `foundry:"default=database"`
	// Stage is a deliberately narrower column: it never holds Archived.
	Stage Status `foundry:"values=Draft|Published"`
}
```

### Add an enum case

A new case always needs a new migration. An applied migration is immutable: its checksum
covers its SQL, so editing the list inside it is drift, and a helper that renders the list
from the enum when the definition is built would change an old migration the day the next
case is added. The new migration writes the constraint again, with every case, as a
literal:

1. Add the constant to the enum and run `foundry generate`.
2. Run `foundry models check ./...`. The finding is printed at the new case. It names the
   table, the column, the constraint (or says the constraint is unnamed) and the file and
   line of the migration that last wrote the list, and prints the list a new migration
   must hold.
3. Decide which of the two alternatives the finding names is true. If the column should
   hold the new case, create the migration with the
   [scaffold command](#development-scaffolding), put the two statements below in its
   `SQL`, list the migration where the application registers its migrations and record its
   checksum wherever the application pins them. If the column deliberately holds only some
   cases, declare them with `values=` on the field instead (see below) and run
   `foundry generate`: the option is part of the model's declaration, so the generated
   output is stale until it is regenerated.
4. Run `foundry models check ./...` again. The newest list now equals the enum.

<!-- compiled-source: tests/fixtures/consumer/enumlists/migrations.go#enum-list-new-migration -->
```go
// ArchivedStatus widens the status list after Archived joined the enum. The
// first migration stays as it was applied; this one drops the constraint and
// writes it again with every case as a literal.
func ArchivedStatus() migrate.Definition {
	return migrate.Definition{
		Key:      migrate.Key{Origin: "consumer.enumlists", ID: ArchivedStatusID},
		Version:  "v0.1.0",
		Requires: []migrate.Key{CreateDocuments().Key},
		SQL: []string{
			`ALTER TABLE enum_list_documents DROP CONSTRAINT enum_list_documents_status_check`,
			`ALTER TABLE enum_list_documents ADD CONSTRAINT enum_list_documents_status_check CHECK (status IN ('draft', 'published', 'archived'))`,
		},
	}
}
```

The two statements drop the constraint and add it again with every case. Use the
constraint's name from the finding. When the finding says the CHECK is unnamed, it prints
the name PostgreSQL gives such a constraint by default; drop it with
`DROP CONSTRAINT IF EXISTS` under that name (see [unnamed constraints](#unnamed-check-names))
and add the new one with an explicit name. With a worked example, for a
`permissions` column that gained `pages.export`:

```sql
ALTER TABLE admins DROP CONSTRAINT admins_permissions_check;
ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (jsonb_typeof(permissions) = 'array' AND (permissions = '["*"]'::jsonb OR permissions <@ '["admins.read","admins.manage","pages.export"]'::jsonb));
```

Keep the other conditions of the constraint and replace only the list. The list a finding
prints holds every case; a value that an equality branch beside the list already admits, as
in `permissions = '["*"]'::jsonb OR permissions <@ '[...]'::jsonb`, may stay in it.

A finding for a case a constraint rejects is one line:
`enums.go:10:2: [enum-list] enum Permission: case "pages.export" (PermissionPagesExport) can never be stored: it is rejected by constraint admins_permissions_check on admins.permissions, last written at migrations.go:8; either (a) the column should hold the new case: add a new migration that re-declares the constraint with this list: '["*","admins.read","admins.manage","pages.export"]'; or (b) the column deliberately holds only some cases: declare values=PermissionAll|PermissionAdminsRead|PermissionAdminsManage on Admin.Permissions and run foundry generate`.
Neither alternative is the default: the first is right when the new case is a case the
column should accept, the second when the list was a narrowing from the start (a column
that marks only some of an enum's cases, for example).

### What the check decides, and what it cannot see

For each enum case the check evaluates every live CHECK constraint that mentions the
column, with the column set to that case's stored value, and decides one of four things:

- **false**: no row can pass the constraint, so the case can never be stored. This is the
  finding "case X can never be stored"; a case several constraints reject is reported once,
  naming each of them.
- **true**: every row passes. No finding. A CHECK also passes an expression that is NULL:
  `CASE WHEN status = 'draft' THEN true END` is NULL for every other case, so it admits them.
- **depends on other columns**: no finding. A part of the expression that does not mention
  the column (`paid_at IS NULL OR status IN (...)`, a transition table of
  `action = 'x' AND from_status = 'y'` branches, `(status IN ('paid')) = (paid_at IS NOT NULL)`)
  neither proves nor refutes, so an `OR` with such a part can always pass and an `AND` with
  it follows the part that mentions the column.
- **cannot be read**: a finding that names the sub-expression, unless the rest of the
  expression or another constraint already decides the case without it. A test of the
  column the check does not evaluate is never passed in silence.

The three SQL values are kept apart. An expression that is NULL for every row (a constant
`NULL`, a comparison with `NULL`, a `CASE` that takes no branch and has no `ELSE`) is neither
true nor false, and it is not "other data" either: a CHECK passes it, `IS TRUE` and
`IS FALSE` of it are false, `IS NOT TRUE`, `IS NOT FALSE` and `IS UNKNOWN` of it are true,
and `NOT`, `AND` and `OR` follow SQL's three-valued tables. So
`(CASE WHEN status = 'draft' THEN true END) IS TRUE` rejects every case except `draft`.
An `IN` or `ANY` list with a `NULL` member is true or NULL for a case and never false, a
`NOT IN` or `ALL` list with one is false or NULL.

The check evaluates the column compared with literals, with qualifiers, parentheses and
casts that cannot change the value being compared around the column: `::text`, `::varchar`
and `::character varying` without a length on a text enum, an integer type on an integer
enum, and `::jsonb` on a collection column or a JSON literal. A cast with a length or
precision (`status::varchar(3)`, `status::char(1)`, `::bpchar`, which pads) or between
families (an integer column cast to `text`, text cast to an integer) can change what is
compared, so it makes the test unreadable. The forms are `=`, `<>`, `!=`, `IN`, `NOT IN`, `= ANY (ARRAY[...])` and
`<> ALL (ARRAY[...])` (hand-written or as `pg_dump` and `\d` print them,
`((col)::text = ANY ((ARRAY['a'::character varying, ...])::text[]))`), `BETWEEN` and
`NOT BETWEEN`, the ordering operators `<`, `<=`, `>` and `>=` for an integer-backed enum,
`IS NULL` and `IS NOT NULL`, and `IS TRUE`, `IS FALSE`, `NOT`, `AND`, `OR` and `CASE` in both
forms (`CASE WHEN ... END` and `CASE col WHEN ... END`) by evaluating their branches. A range
is not enumerated, so no cost depends on its size. For a collection column (a slice, array
or `value.JSON` of an enum, stored as a `jsonb` array) a case is storable when some array
that holds it passes, and the check evaluates under that meaning. `col <@ '["a","b"]'::jsonb`
is decided by membership of the case in the literal; `col @> '["a"]'::jsonb` is true when the
literal holds nothing but the case and depends on the other elements otherwise;
`col = '["*"]'::jsonb`, the form an application uses for "all", is true when the case is a
member of the literal; `col <> '[...]'::jsonb` depends on the other elements;
`jsonb_typeof(col)` is `'array'`; and every `jsonb_array_length(col)` test depends on the
other elements, except a test that only the empty array passes (`= 0`, `< 1`, `<= 0`,
`BETWEEN 0 AND 0`), which is false because the array holds the case.

Anything else that mentions the enum column cannot be read, and the finding says which
part: a function applied to the column (`lower(col)`), a regular expression (`~`),
`SIMILAR TO`, `LIKE`, arithmetic, a subquery, a concatenation, a function or arithmetic of
another column (`col = lower(other)`), the ordering of text (it depends on the collation), a
JSON test other than the two above, a list member that is not a string or integer literal,
a number that does not fit in 64 bits, `E'...'` strings with an escape PostgreSQL rejects
(the others are decoded as PostgreSQL does), and an expression nested more than 32 levels or
with more than 500 `AND`, `OR`, `NOT`, `IS` and `WHEN` words. The fix is a new migration that
drops the constraint and writes it again with tests that only compare the column with
literals, such as a plain `IN (...)`, `= ANY (ARRAY[...])` or `<@ '[...]'`. For a constraint
the migrations named, the finding prints its name; for an unnamed one it prints
PostgreSQL's default name for it (see [unnamed constraints](#unnamed-check-names)) in
`DROP CONSTRAINT IF EXISTS`, and a name for the new constraint. Every live constraint is
evaluated, so the finding ends when the constraints that remain can all be read: an
unreadable constraint that the migration does not drop keeps it.

The other direction is checked too. The check first evaluates the column's constraints with
the column set to a value that equals none of their literals (every `=`, `IN` and `<@`
membership test is false for it, every `<>` and `NOT IN` true, an ordering or range test
unreadable): unless that is definitely false the column is open, any value can be stored, and
nothing is reported. When it is false the column is closed to a list, and each literal that
a membership test compares it with, that is not an enum case, is a finding at the model
field, "holds X, which is not an enum case", unless the constraints reject it or depend on a
test the rule cannot read for it. So `status <> 'archived' OR n > 1` and
`col BETWEEN 0 AND 5` hold nothing, and `status IN ('a', 'b', 'x') AND n > 0` holds `x`.
A range test (`BETWEEN` and the ordering operators) never reports a value that is not an
enum case: its bounds are not values the column holds. Non-case values are collected from
positive membership tests only (`=`, `IN`, `= ANY` and `<@`); a value that only a negated
test admits (`<>`, `NOT IN`, `<> ALL`) is not reported. A cast that could change the value
being compared (see above) makes the test unreadable, so no value is reported through it;
that is by design.
A column that
declares `values=` has its named cases checked as above, and every case it does not name
must be rejected by some constraint: a case that every constraint passes is a finding.

A comparison of the column with another plain column (`type = col`,
`from_level <> to_level`) depends on that column's value, so it counts as depending on other
columns. The check does not reason across columns, because it evaluates one column at a
time: a constraint that ties the enum column to another column whose own values a second
constraint restricts is not detected (`CHECK (status = other AND status IN (...))` beside
`CHECK (other = 'draft')` leaves `status` able to hold only `draft`, and nothing is reported).

The check reads CHECK constraints and nothing else. It cannot see, by design, a trigger that
enforces a list, a foreign key to a lookup table, a generated column derived from the
enum column, the bounds of a partition (`PARTITION BY LIST` with
`PARTITION OF ... FOR VALUES IN`), an exclusion constraint, a rule, or a function the
database calls. A column whose type is a `CREATE DOMAIN` or `CREATE TYPE ... AS ENUM`
created in the history is reported as unreadable, because the values live in the type: add
a literal `CHECK` on the column. Such a constraint is never a finding when it is missing:
the database constraint is optional. A CHECK on a column whose type the check does not
know to compare exactly is unreadable too: `citext` compares without case, an array column is not a JSON array. A `char(n)` or
`bpchar` column is read exactly when every case value and every literal compared with it has
at most n characters and no trailing space, because PostgreSQL pads its values and ignores
trailing spaces when comparing them; otherwise it is unreadable. The types it compares exactly are `text`, `varchar`,
the integer types and `jsonb`; give the column one of them. A `CHECK ... NOT ENFORCED`
constraint (PostgreSQL 18) is not evaluated, because the database does not apply it, until
`ALTER TABLE ... ALTER CONSTRAINT name ENFORCED` makes it count. A `CREATE TABLE IF NOT
EXISTS` of a table the migrations already created changes nothing, as in PostgreSQL. A
constraint that mentions no column (`CHECK (NULL IS TRUE)`) bears on every column of its
table.

The same "cannot be read" finding is made when a statement the check cannot follow may have
written or changed a constraint that compares the column with literals: a `DO` block that
names the constraint, table or column, a `RENAME` of a column or constraint the table
holds, `ALTER TABLE ... SET SCHEMA` or `INHERIT`, `ALTER SCHEMA`,
`CREATE TABLE ... (LIKE ...)`, `CREATE TABLE ... AS`, `INHERITS` or `PARTITION OF`, a
statement that changes `pg_constraint`, and a migration that sets `search_path` (every
unqualified table name in that migration is unreadable).

Other findings and what is not one:

- A literal that is not an enum case is a finding at the model field. Remove it in a new
  migration with the same two statements.
- A column that deliberately holds fewer cases declares them on the field with
  `foundry:"values=Draft|Published"`, naming the cases by their Go constant names, then
  runs `foundry generate`. Every constraint must then reject each case the tag leaves out
  and admit the named ones, so a new enum case does not touch the column, and a case the
  database admits but the tag leaves out is still found. The option is refused on a field
  that holds no enum, with a name that is not a case, and when it names every case. See
  [enum columns and their lists](model-declarations.md#recipe-enum-narrowing).
- A table is identified by schema and name: a table written without a schema is in
  `public`, a model's `table=` may be `schema.table`, and a statement about
  `archive.invoices` never touches `public.invoices`. A quoted name keeps its case and an
  unquoted one is folded to lower case, as in PostgreSQL.
- A column that no CHECK constraint mentions is not a finding.
- A migration whose SQL is built at run time (not written as string constants) is a
  finding when the text it does show names a table of a model.
- Lists inside functions and triggers, `DO` blocks that do not name the table, constraint
  or column, constraints on other tables, and a database enum type or domain nothing in the
  history uses for an enum column are not read. The check reads `CREATE TABLE`,
  `ALTER TABLE ... ADD [CONSTRAINT] CHECK`, `ADD COLUMN ... CHECK`, `DROP CONSTRAINT`,
  `DROP COLUMN`, `ALTER COLUMN ... TYPE`, `RENAME TO` and `DROP TABLE`. Each migration SQL
  string is limited to 1 MiB and a module to 10,000 migrations.

A clean run says how many enum columns it compared with their CHECK constraints; a column
with no CHECK constraint on it is not one of them.

<a id="unnamed-check-names"></a>
### Unnamed constraints

A finding names a constraint when the migration names it (`CONSTRAINT admins_permissions_check
CHECK (...)`). For an unnamed `CHECK` it says "the unnamed CHECK" with its file and line,
and where a statement has to drop it, it prints the name PostgreSQL gives such a
constraint by default: `<table>_<column>_check` when the expression uses exactly one column
and `<table>_check` otherwise. When a constraint of the same table already holds the name,
PostgreSQL puts a number after `check` (`invoices_status_check1`), and the check does the
same; both names are shortened to fit 63 bytes as PostgreSQL does. The name is the one for
that statement, assuming the schema held no constraint of that name before: a constraint of
another table with the same name makes PostgreSQL choose a numbered one, which the check
cannot see. Naming constraints explicitly in your migrations removes the doubt.

`DROP CONSTRAINT [IF EXISTS] name` removes an unnamed constraint from what the check reads
only under that exact name. Any other name leaves the constraint in force: with `IF EXISTS`
PostgreSQL does nothing, and without it the migration fails. So
`DROP CONSTRAINT IF EXISTS invoices_status_check` does not drop an unnamed check over
`status` and `n`, whose name is `invoices_check`, and the case it rejects is still found.
A `DO` block that may have dropped an unnamed constraint (it looks the name up in
`pg_constraint`) leaves the check unable to tell; `DROP CONSTRAINT IF EXISTS` under the
default name removes the constraint from what the check reads whether or not the block
already did, and a constraint written after the block does not.

<a id="recipe-register-migration"></a>
## Register every migration

A migration that exists but is missing from the application's list is neither run nor
noticed. `foundry models check` reports each migration function that is never used outside
another migration's literal (the `Requires` of a later migration does not register it). A
migration function has no parameters and its body is `return migrate.Definition{...}`, or
one local declared from the literal and returned unchanged. A call registers it, and so
does a reference to the function as a value, as in `[]func() migrate.Definition{M2}`:

```text
migrations.go:38:6: [migration-unregistered] migration Forgotten (app/006_forgotten): is never listed with the application's other migrations, so it never runs; add Forgotten() to the list of migrate.Definition values the application registers, or delete the file. Foundry guide: migrations-and-seeding#recipe-register-migration
```

Add the call to the list the application passes to `migrate.New`, or delete the file. A
function that is never registered is also left out of the history the enum check replays.
Definitions written inline in a `[]migrate.Definition` literal cannot be forgotten and are
not checked. A function that does anything else with the definition (changes it before it
returns, or takes parameters) is a builder: the check cannot tell it is a migration, so it
never reports it.

<a id="recipe-run-migrations"></a>
## History and explicit execution

`registry.Inspect(history)` returns copied status entries and all detected problems. It validates record shape, checksum encoding, positive batch numbers, and timestamps. Changed or missing definitions and unapplied dependencies block `Report.Check` and `registry.Pending`. A caller-owned report is informative; it is never authorization to skip the runner's locked history check.

`MigrationStatus` reads the history without changing it; `database` is the framework's `database` package:

<!-- compiled-source: tests/fixtures/consumer/articles/migration_recipes.go#inspect-migrations -->
```go
func MigrationStatus(ctx context.Context, db *database.DB, registry *migrate.Registry) (migrate.Report, error) {
	runner, err := migrate.NewPostgres(db, registry, migrate.DefaultPostgresConfig())
	if err != nil {
		return migrate.Report{}, err
	}
	return runner.Status(ctx)
}
```

After reviewing status and choosing to apply, call:

<!-- compiled-source: tests/fixtures/consumer/articles/migration_recipes.go#apply-migrations -->
```go
func ApplyMigrations(ctx context.Context, runner *migrate.Postgres) (migrate.RunResult, error) {
	return runner.Up(ctx)
}
```

Constructing the runner performs no I/O. `Status` only reads committed history; an absent history table yields pending definitions without creating a table or taking a migration lock. `Up` acquires a session advisory lock, rereads and validates history, then selects executable pending definitions. When work remains, it ensures its history namespace and executes each selected definition according to its declared mode. The default commits SQL and history in one transaction. It never synchronizes schema during ordinary application boot.

All cooperating runners must use the same configured history schema and table. Defaults are `foundry_ops.schema_migrations`, a 30-second lock wait, 50-millisecond polling, five-second cleanup, at most 10,000 history records, a 10-second `StatementLockTimeout` and no `StatementTimeout`. An optional `SearchPath` names one schema that `Up`, `Rollback` and `Reconcile` set as their session `search_path` (followed by `pg_temp`, as a scoped pool does), so unqualified migration SQL creates and alters objects in that schema; empty keeps the connection's own path. History and progress tables are always schema-qualified.

`Up` sets PostgreSQL's `lock_timeout` to `StatementLockTimeout` on its session, so DDL that cannot obtain a table lock fails with `LockNotAvailable` instead of queueing behind a long transaction and blocking every later query on that table. A transactional migration then rolls back, and the error names the migration and says it was not committed and can be retried once conflicting transactions finish. `StatementTimeout` optionally bounds each migration statement; zero disables it, so long migrations are not canceled by an application pool limit. Both settings are reset before the connection returns to the pool. Failures name the migration and the next safe action: checksum drift and reconciliation messages keep their own text, serialization/deadlock conflicts are retryable, and an unknown commit outcome asks you to run `migrate status` and reconcile before retrying. Identifiers are quoted and restricted to simple names of at most 63 ASCII bytes to prevent PostgreSQL name truncation. Reads request at most the configured history limit plus one row and fail on overflow. Before executing migration SQL, `Up` also rejects a pending set that would grow history beyond that limit. PostgreSQL's [identifier rules](https://www.postgresql.org/docs/18/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS) and [session lock behavior](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS) define these boundaries.

Concurrent runners serialize and reload history after acquiring the lock. Earlier migrations remain committed if a later one fails. `RunResult.Applied` lists confirmed commits; `Interrupted` identifies an attempted migration without a successful confirmation. With an unknown commit outcome, that attempt may have committed. A later explicit `Up` reconciles from history under the lock. A cleanup error can accompany fully applied results; inspect both result and error.

Failed lock cleanup discards the physical connection, releasing session-owned locks when the server session ends. Caller cancellation still governs the session lifetime; a new cleanup deadline cannot revive a canceled session. There is no reset, wipe, or destructive schema synchronization command. Test against an isolated project database and never reset existing data.

## Explicit rollback

These recipes need no application model; the transport and helper types they use are declared or described on this page.

A definition may declare `Down` SQL that reverses it:

<!-- compiled-source: tests/fixtures/consumer/migrations/guide_rollback.go#reversible-migration -->
```go
definition := migrate.Definition{
	Key:     migrate.Key{Origin: "app", ID: "20260911000001_add_record_label"},
	Version: "v0.1.0",
	SQL:     []string{"ALTER TABLE records ADD COLUMN label text"},
	Down:    []string{"ALTER TABLE records DROP COLUMN label"},
}
```

`Down` is excluded from the checksum, so adding or correcting it never drifts applied history; `Entry.Reversible` reports it. It must be valid inside one transaction, so a nontransactional migration cannot declare it. `runner.Rollback(ctx, steps)` reverses the last `steps` applied migrations, newest first (latest batch, then reverse registry order), each in its own transaction that runs `Down` and deletes its history row, while holding the migration lock. It refuses before changing anything when history has drifted or awaits reconciliation, when a selected migration belongs to a disabled feature, when it has no `Down`, or when a migration outside the selection depends on one inside it. Earlier reversals stay committed if a later one fails; `RollbackResult.Interrupted` names an unconfirmed reversal. `runner.RollbackPlan(ctx, steps)` reports the same selection without locking or changing anything.

Rollback selection includes all applied migrations, including disabled features. If the requested selection includes a disabled feature, both `RollbackPlan` and `Rollback` refuse the whole selection before running any `Down` SQL. They never skip it to reverse an older migration. Enable the feature before explicitly rolling it back. An unrelated selection remains eligible.

Rollback is never implicit and is destructive by nature. `migrate rollback --step N` (default 1) prints the plan and exits with an error; only `--confirm` executes it. Rolled-back migrations become pending again, and a later `migrate up` reapplies them.

<a id="recipe-seed-data"></a>
## Transactional seeders

Handwritten ORM declarations use the same `seed.Definition.Run(ctx, tx)` callback.
Call `orm.Insert` with that supplied transaction and typed assignments;
the normal defaults and write lifecycle apply. The [ORM seeder example](../../tests/fixtures/goway/integrations/seeding/seeder.go)
uses this path without a generated draft or changes to the seeder registry.
For repeatable reference data, supply a stable key and preserve existing values
with an explicit conflict policy. `Author` is the application's model and `Authors`
its generated table; `database` is the framework's `database` package. `AuthorSeeder` inserts one author if absent, and `SeedAuthors` runs it:

<!-- compiled-source: tests/fixtures/consumer/articles/seeding.go#seed-reference-data -->
```go
func AuthorSeeder(id model.ID[Author], name string) seed.Definition {
	return seed.Definition{ID: "articles.author", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Upsert(ctx, tx, Authors.Query(),
			orm.OnConflict(orm.ConflictKey(Authors.Key())).DoNothing(),
			orm.Set(Authors.Key(), id), orm.Set(Authors.Name, name))
		return err
	}}
}

func SeedAuthors(ctx context.Context, db *database.DB, id model.ID[Author], name string) (seed.Result, error) {
	registry, err := seed.New(AuthorSeeder(id, name))
	if err != nil {
		return seed.Result{}, err
	}
	return registry.Run(ctx, db, "articles.author")
}
```

`seed.New` validates named `seed.Definition` values and their dependencies. Each definition has a typed `seed.ID` and `Run func(context.Context, *database.Tx) error`. Use the supplied transaction for all work. The registry snapshots dependency lists and orders them deterministically before execution.

`registry.Run(ctx, db, selectedIDs...)` expands selected seeders to include prerequisites. Omitting selection runs the full registry. Invalid/duplicate selection fails before database I/O. Every seeder has a separate transaction and uses the normal [transaction lifecycle](database-runtime.md#transaction-scopes), including rollback on callback error/panic and after-commit behavior.

`seed.Result.Committed` lists confirmed commits. `StoppedAt` identifies the failing invocation and can also appear in `Committed` if only after-commit work failed. Earlier seeders remain committed. Foundry never retries arbitrary business callbacks or silently suppresses a later explicit invocation. Repeat execution and idempotence are the seeder's domain contract; neither is implemented by resetting tables. Shared callbacks must be safe if callers invoke a registry concurrently.

## Consumer commands

The public `database/command` package owns parsing and reporting for:

```text
migrate status [--database name] [--schema name] [--format text|json]
migrate up [--database name] [--schema name] [--format text|json]
migrate rollback [--database name] [--schema name] [--step N] [--confirm] [--format text|json]
migrate show --migration origin/id [--database name] [--schema name] [--format text|json]
seed list [--format text|json]
seed run [--database name] [--format text|json] [--id app.regions --id app.roles]
prune list|run ...   (see model pruning; prune run also accepts --database)
```

The [consumer command example](../../tests/fixtures/consumer/database_command_test.go) compiles these APIs. Parse with `command.Parse(args, stderr)` before assembling services. Help returns `flag.ErrHelp`; invalid arguments fail before database use. Call the resulting invocation's `Run(ctx, resources, stdout)` with `command.Resources` containing the migration runner, seeder registry, and database required by that command. Resource ownership and shutdown stay with application assembly. `seed list` only needs the registry. A configured application normally lets [`RunDatabaseCommand`](#application-migration-targets) assemble these resources.

`Resources` takes either one `Migrations` runner, whose output is unchanged, or `MigrationTargets`: the per-schema runners of one database, named by `DatabaseName`. `--database` must match `DatabaseName`, so resources assembled without a name refuse a selection instead of silently ignoring it, and `invocation.Database()` tells the assembling code which connection to open. With targets, `status` reads every schema in name order, `up` applies them in that order and stops at the first failure (earlier schemas stay committed), `--schema` selects one, and `rollback` refuses to guess: it needs `--schema` when the database has several. Text output heads each target with `== database/schema`; JSON wraps per-target reports as `{"database": ..., "targets": [{"schema": ..., ...}]}`, and rollback output names its single target.

These commands run in the consumer's compiled binary, which owns its declarations and configuration. The separately installed `foundry` development tool owns generation/scaffolding and does not dynamically load an application registry. The later CLI kernel will host these same feature commands through shared bootstrap.

Status prints the report and returns a conflict error when history has drift. Migration and seeder execution print confirmed progress even if later work fails; the returned error preserves the database outcome. An output-writer failure can follow committed work and reports the confirmed count. Neither command retries an operation because output failed. Status, execution and rollback JSON contains typed metadata and progress, never migration SQL, credential values, or unwrapped database causes. `migrate show` is the explicit exception for reviewed SQL: it prints one definition's identity, version, mode, checksum, prerequisites, `SQL` and `Down` from the registry, per matching schema target, without database I/O. Use it to read a framework feature's migrations, such as `--migration foundry.outbox/000001_create_messages`; framework definitions are applied from the framework, not copied into the application, so the checksum always matches the code that reads the tables. `invocation.NeedsDatabase()` is false for `migrate show`, `seed list` and `prune list`, so `RunDatabaseCommand` does not connect for them. Commands propagate the caller's context.

<a id="recipe-migration-targets"></a>
## Application migration targets

`app.Migrations()` returns runnable framework and application definitions, grouped by physical database and schema. Declare application migrations with `Builder.Migrations`. Runtime feature switches control execution, not historical identity.

Each runnable snapshot carries immutable disabled-history metadata. The existing loop using `migrate.New(target.Definitions...)` and `runner.Up(ctx)` remains supported without an option or extra argument. `Definition.Clone` preserves that context. Disabled SQL is absent from the runnable slice, so existing SQL enumeration also stays safe. `Definition.Equal` compares declarations without comparing the attached context. It ignores dependency order and treats nil and empty slices alike; SQL and `Down` statement order still matters.

`app.MigrationHistory()` returns the complete physical groups, including schemas with only disabled features. For each group, `migrate.New(group.Definitions...)` constructs its full inspection registry. `RunDatabaseCommand` uses these same immutable registries for status, up, show and rollback, including console commands. The PostgreSQL application testkit's `Environment.Start` also reads `MigrationHistory()`, then merges any explicit domain targets through `DatabaseSettings.MigrationGroups`. An unrelated domain target can therefore extend a schema whose framework features are all disabled. Repeating a domain declaration already registered with `Builder.Migrations` is supported, including dependencies declared in a different order from the registry snapshot. Custom test applications that only implement `Migrations()` keep using that path.

The testkit includes complete history only inside its owned namespaces. It does not open disabled-only histories outside them, such as an unchanged archive `public` schema. Runnable migrations and explicit domain targets outside those namespaces still fail validation before database I/O.

`migrate.MergeDefinitions` owns the composition rule used by both `migrate.New` for carried history and `DatabaseSettings.MigrationGroups` for shared targets. Repeated keys must agree on version, execution mode, ordered SQL, ordered `Down`, and dependencies regardless of dependency order. Any available declaration makes an otherwise matching shared migration available. Explicit definitions cannot override conflicting carried history. `migrate.New` still rejects duplicate explicit keys; target grouping collapses matching repeated declarations.

Composition compares `Down` even though persisted checksums exclude it: two declarations supplied together must agree on what an explicit rollback would execute. Adding or correcting `Down` in a single authoritative declaration between releases remains supported without checksum drift.

Framework definitions carry typed `migrate.Availability` metadata. `migrate.Available` is the zero value; `migrate.FeatureDisabled` prevents execution. Availability is excluded from checksums. With a feature disabled, applied migrations remain `migrate.Complete` and still undergo checksum and dependency checks. Unrun disabled definitions report `migrate.PendingDisabled` in the full Go inspection snapshot. Console text labels them `Disabled:` with state `pending_feature_disabled`. The typed `migrate.StatusReport` output places them in each target's `disabled_pending` array, with the same `key`, `state`, and definition metadata. `targets[].migrations[]` keeps runnable pending entries and all applied history, including disabled features as `applied`. A disabled-only target has `migrations: []`. Older deploy tools ignore the new field and do not request reconciliation or run migrations for these entries. Drift, missing definitions, and incomplete history still block deployment through the existing status and problem fields. `Registry.Pending` and `Postgres.Up` skip only the unrun disabled definitions. Re-enable the feature to apply its pending definitions normally. Application-owned definitions remain available by default.

Deploy release plans and root host reports expose a `migrations` summary with a runnable `pending` boolean and per-schema `targets[].disabled_pending` entries. Public summaries include only each entry's `key.origin`, `key.id`, and `state`. Reports return `migrations: null` when inspection is unavailable; a failed probe records `migration_status_unavailable`. Recovery and rehearsal use the same runnable-pending decision. `App.MigrationHistory()` and the application testkit retain complete inspection history, so neither loses disabled definitions.

This applies to sessions, tokens, MFA, idempotency, outbox, audit, telemetry, notifications, webhook capture declarations, extensions, translations, attachments and job archives. A configured cache store retains its PostgreSQL definitions when switched to another driver. Shared cache migrations remain available while any store on that target uses PostgreSQL. Outbound capture keeps its existing store semantics: disabling recording leaves the viewer and pruning store available, so its migrations remain executable whenever that store is present.

Keep each feature's database and schema settings when disabling it. They identify the history to inspect and remain validated when databases are configured. Removing a connection or moving a feature to another schema does not move its history. Applications without database connections contribute no disabled framework targets. `WithDatabaseScopes` also aligns disabled feature targets so later toggles keep the same history. It preserves `Worker.Archive` exactly, including an explicit or default `public` schema; configure a different archive schema explicitly.

An available pending migration that requires an unapplied disabled migration reports `migrate.DependencyDisabled` and blocks execution. Foundry never enables a prerequisite implicitly. Applied prerequisites remain usable while their feature is disabled. Interrupted nontransactional migrations retain their progress and reconciliation requirements; disabling one does not resume its SQL. A reconciled disabled checkpoint does not lower the batch number of unrelated pending migrations. Re-enabling the feature resumes its own checkpoint at the recorded batch. An `Up` with no executable pending work creates no history or progress tables.

`RunDatabaseCommand`, in the application's `migrations` package, registers its definitions and runs one parsed command. `settings` is the application's `application.Settings`, `args` the command-line arguments:

<!-- compiled-source: tests/fixtures/consumer/migrations/database_command.go#application-migrations -->
```go
func RunDatabaseCommand(ctx context.Context, settings application.Settings, args []string, stdout, stderr io.Writer) (err error) {
	invocation, err := dbcommand.Parse(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	app, err := application.New(settings).Migrations(infrastructure.MigrationTarget{Definitions: Definitions()}).Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
		defer cancel()
		err = errors.Join(err, app.Shutdown(shutdown))
	}()
	return app.RunDatabaseCommand(ctx, invocation, application.DatabaseCommandResources{}, stdout)
}
```

Each `infrastructure.MigrationTarget` names a connection (blank selects the default) and a schema (blank selects the connection's primary schema, or `public` for an unscoped connection). `DatabaseSettings.MigrationGroups` is the one resolution used by `Build`, `RunDatabaseCommand` and the PostgreSQL testkit. Targets addressing the same primary host, port, database and schema share one history, so they merge: a repeated identical definition collapses, a conflicting one fails, and each group must be a valid `migrate` registry. An undetected alias, such as a second hostname for one server, still shares the history table; the runner's drift check then refuses to apply rather than guessing.

`RunDatabaseCommand` needs only a built application. It selects `--database` or the default connection, never every connection; opens only that connection's primary pool; never starts providers, so unrelated services need not be reachable; and closes the pool before returning. Every history group on that database gets a runner whose history is `<schema>.schema_migrations` and whose `SearchPath` is that schema, so framework features configured for a schema other than the connection's are created where their stores read them. This is also where the PostgreSQL testkit keeps history. History written by a hand-built runner with `DefaultPostgresConfig` lives in `foundry_ops.schema_migrations` instead, which the helper does not read; keep using that runner for such a deployment instead of letting the helper see an empty history. `seed run` and `prune run` use the same pool with the registries in `DatabaseCommandResources`; the pool carries the application's `Encryption` key ring, so [encrypted model fields](encryption.md#encrypted-model-fields) seal and open as they do in the running application.

## Development scaffolding

From a consumer workspace, select an existing Go package with current generated output:

```sh
foundry make migration --dir migrations --name AddRecordLabel \
  --id 20260911000001_add_record_label --origin app --version v0.1.0
foundry make seeder --dir seeders --name SeedRegions --id app.regions
```

Inside the framework repository, use `go run ./cmd/foundry make ...` with the same flags. These commands create one `20260911000001_add_record_label_migration.go` or `seed_regions_seeder.go` file. A migration file starts with its ID, so a directory lists migrations in their conventional run order; the fixed `_migration.go` suffix keeps it an ordinary Go file whatever the ID ends with. `--name` still names the Go function and ID constant, and file names never affect checksums. Names must be exported Go identifiers; identity fields follow the registry's semantic grammar. The explicit ID/version makes output repeatable and keeps historical metadata under review. No schema inspection or database connection occurs.

Scaffolds are ordinary Go functions returning typed definitions, with typed ID constants for dependencies and selection. Complete the SQL or seeder domain work, then explicitly register those definitions with `migrate.New` or `seed.New`. A migration scaffold also contains an empty `Down` list; add reviewed reversal SQL there only if the change should support [explicit rollback](#explicit-rollback), otherwise leave it empty and the migration stays irreversible. An unfinished migration fails registry validation; an unfinished seeder returns an error. There is no silent successful placeholder.

The generator checks the complete proposed package in memory before publication and refuses stale generated declarations, name conflicts, path traversal, symlink targets, and existing files. The current command requires an existing Go package; creating a new package directory is outside this scaffold slice. It uses the shared guards, source/module checks, staged writes, and recovery path. A new file is published without replacing a file created concurrently. Existing declarations and package imports are not rewritten.

Once created, a scaffold belongs to the consumer. Edit it normally; it has no generated-file marker and never enters `.foundry-gen.json`. `foundry generate` leaves the implementation alone. Interrupted creation uses the creation-only journal defined in [`internal/generate/recover.go`](../../internal/generate/recover.go) and the ordinary `foundry generate --recover --dir package` command. Recovery preserves a fully published scaffold, clears an unpublished creation, and refuses to overwrite subsequent edits. There is no `--force` overwrite option.

## Verification limits

Protocol tests exercise apply-once history, drift, bounded history reads/growth, concurrent runners, lock timeout, partial commits, lost commit responses, connection disposal, seeder dependencies, and after-commit outcomes. Command tests cover service-free help/listing, drift exit errors, typed selection, progress on failure, and writer failure after commit. Scaffold tests compile a separate consumer, verify that unfinished work fails, and cover deterministic output, ownership, name/file collisions, exclusive creation, and recovery. Consumer metadata tests inspect declarations without SQL. Real PostgreSQL tests additionally run competing migration runners, verify read-only status, reject checksum drift, recover after a failed pending migration, and execute an explicitly repeatable seeder twice. Lost commit responses are tested through protocol fault injection, not a live network interruption.

<a id="recipe-online-migration"></a>
## Concurrent indexes and nontransactional statements

The origin `security-consumer` and the table `consumer_security_records` belong to the example application; use your own migration origin and table name.

`OnlineMigrations` registers a table, then an index created concurrently with `Mode: migrate.NonTransactional`, which depends on the table:

<!-- compiled-source: tests/fixtures/consumer/security/contracts.go#online-migrations -->
```go
func OnlineMigrations() (*migrate.Registry, error) {
	first := migrate.Key{Origin: "security-consumer", ID: "001_records"}
	return migrate.New(
		migrate.Definition{Key: first, Version: "v1", SQL: []string{"CREATE TABLE consumer_security_records (id bigint PRIMARY KEY, label text)"}},
		migrate.Definition{Key: migrate.Key{Origin: first.Origin, ID: "002_online_label"}, Version: "v1", Requires: []migrate.Key{first}, Mode: migrate.NonTransactional, SQL: []string{"CREATE INDEX CONCURRENTLY consumer_security_label_idx ON consumer_security_records (label)"}},
	)
}
```

Execution mode participates in the immutable checksum. The zero/default mode
keeps existing transactional checksums unchanged. Switching an already applied
migration's mode is definition drift; create a new migration instead.

Nontransactional SQL executes on the same locked session, outside a transaction.
Before each statement, Foundry durably records an in-flight checkpoint. After a
confirmed result, it records the number of completed statements. The final history
insert and removal of the progress row are atomic with each other; earlier SQL
has already committed independently. Progress lives in a separate bounded table
in the configured history schema, with a name derived from the history table.

`Status` includes `Progress` on an incomplete migration. A ready checkpoint can
resume its next unstarted statement. An in-flight or uncertain checkpoint blocks
`Up`, even after restart. Earlier effects are preserved and no uncertain SQL is
automatically replayed. A completed checkpoint can retry only final bookkeeping.
Definition drift/removal or missing prerequisite history also blocks an unfinished
migration and its reconciliation. Read a fresh `Status` after an interruption; the
error may carry an earlier snapshot if recording a checkpoint also failed. The
consumer command `migrate status --format json` exposes the full progress record.

After inspecting the real catalog/data, pass that exact progress snapshot to:

<!-- compiled-source: tests/fixtures/consumer/security/contracts.go#reconcile-migration -->
```go
func Reconcile(ctx context.Context, runner *migrate.Postgres, inspected migrate.Progress, outcome migrate.StatementOutcome) (migrate.Progress, error) {
	return runner.Reconcile(ctx, inspected, outcome)
}
```

Reconciliation takes the same advisory lock, checks definition identity and
revision, and records your attestation without executing migration SQL. Stale
snapshots are rejected. Inspect again after an ambiguous reconciliation result.
A wrong attestation can repeat or omit effects; partial effects require operator
repair before choosing either outcome. Concurrent index failure can leave an
invalid index. Foundry never drops it or resets a database automatically.

Ordinary historical definitions remain transactional. Do not put explicit
transaction-control statements in either mode, and keep each nontransactional
entry to one reviewed SQL statement. Consumers own SQL semantics and deployment
coordination; typed declarations cannot prove a statement is safe to repeat.
