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
own setup. The remaining generated relation/source-write tests migrate in B3.
