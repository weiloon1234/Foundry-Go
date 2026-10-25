# Link query fixture

ORM declarations own persistence and lifecycle callbacks.
Use `Members`, `Groups`, `Memberships`, `NaturalLinks`, `NullableLinks`,
`Friendships` and `Archives`. `MemberLinks` holds the four pivot relations.

`Memberships.Role` accepts fresh string input. `Archives.Tag` accepts
`ArchiveTagInput`; predicates still use stored strings. Managed timestamps use
`CreatedAt`/`UpdatedAt` roles. Force deletion requires the matching `.Soft` token.
`Members.ID` and `Archives.ID` are primary mapping columns for insert-select;
ordinary key lookup uses `.Key()`.

Register `orm.NewObserver[Membership]` with `ORMHooks` when using these tables.
Local hooks and retrieval callbacks are already attached. Each fixture application
selects only the ORM observer adapter family.

Source insert/update/delete, pivot lifecycle, nullable and natural keys, and lookup
concurrency use ORM/rows. `runORMLinks` registers only ORM observers. Source operations preserve set-write
semantics and `ForceDeleteUsing` receives `Memberships.Soft` explicitly.

The bounded per-model batch suite now uses ORM `RestoreEach` and
`ForceDeleteEach` with the declaration's `.Soft` token, including natural keys.
Lookup branch, scope, failure and concurrency assertions use sealed assignments.
The obsolete application creation-adapter test is retired: lookup laziness remains
covered by `TestPostgresLookupWritesChooseOneLifecycleBranch` and the framework's
`TestLookupExistingSkipsUnchosenDraftAndRetrieval`. Read SQL and bindings compare
against `testdata/legacy-read.json`, captured through the old API at `0ada5ac7`.

Group's model directive, generated output and legacy `groupReads` factory remain
only for lane D's `reference_natural_key` negative, which still calls
`Group.FoundryReference`. The native application uses ORM declarations exclusively.
