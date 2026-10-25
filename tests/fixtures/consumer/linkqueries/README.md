# Link query fixture

The ORM declaration surface is published alongside the retained generated API.
Use `Members`, `Groups`, `Memberships`, `NaturalLinks`, `NullableLinks`,
`Friendships` and `Archives`. `MemberLinks` holds the four pivot relations.

`Memberships.Role` accepts fresh string input. `Archives.Tag` accepts
`ArchiveTagInput`; predicates still use stored strings. Managed timestamps use
`CreatedAt`/`UpdatedAt` roles. Force deletion requires the matching `.Soft` token.
`Members.ID` and `Archives.ID` are primary mapping columns for insert-select;
ordinary key lookup uses `.Key()`.

Register `orm.NewObserver[Membership]` with `ORMHooks` when using these tables.
Local hooks and retrieval callbacks are already attached. Each fixture application
selects exactly one observer adapter family; the retained legacy suite uses its
own setup.

Source insert/update/delete, pivot lifecycle, nullable and natural keys, and lookup
concurrency now use ORM/rows. `runORMLinks` registers only ORM observers; the retained
legacy tests use a separate application. Source operations preserve set-write
semantics and `ForceDeleteUsing` receives `Memberships.Soft` explicitly.

The bounded per-model batch suite now uses ORM `RestoreEach` and
`ForceDeleteEach` with the declaration’s `.Soft` token, including natural keys. The legacy runtime creation
adapter oracle is isolated in `legacy_creation_bridge_postgres_test.go`; ORM
lookup branch, scope, failure and concurrency assertions use sealed assignments.
