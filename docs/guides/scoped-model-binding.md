# Alternate keys and nested model binding

T04 native verification passed. Acceptance status is owned by the
[master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery).
The [independent consumer](../../tests/fixtures/consumer/nestedbindings/bindings.go)
contains complete typed declarations and a three-level handler.

## Bind declared keys and relationships

```go
teams := modelbinding.ByKey(db, QueryTeams(),
    func(p Path) model.ID[Team] { return p.Team })
projects := modelbinding.Through(db, teams,
    TeamRelations().Projects.Where(ProjectFields().Enabled.Eq(true)),
    ProjectFields().Slug, func(p Path) Slug { return p.Project })
```

The resolver returns `modelbinding.Models[Team, Project]` with concrete `Parent`
and `Child` fields. The team resolves once. Project lookup adds the relationship's
stored parent key to its existing scope; there is no global project fallback.
Two teams may have the same project slug. Missing parent/child returns 404; SQL,
codec and retrieval-hook failures remain errors and publish no partial bundle.

For an alternate key without a parent, use
`modelbinding.ByField(db, QueryProjects(), ProjectFields().Slug, selectSlug)`.
The stored field preserves both model owner and key type. Duplicate keys in the
selected scope are an internal configuration/data error. The shared
`query.Unique`/`query.RelatedUnique` owner checks at most two rows in one SELECT,
avoiding a separate count/find race. Database unique constraints are still
recommended. Limit/offset scopes reject because they could hide duplicates.

`Through` accepts generated direct relationships: BelongsTo, HasOne and HasMany.
Their filters, soft deletion, eager loading and retrieval hooks remain active.
It does not reinterpret a many-to-many pivot as a direct foreign key. Use `Then`
with an explicit typed custom resolver when the domain relationship is different.

## Add another level

```go
tasks := modelbinding.ThroughSelected(db, projects,
    func(previous modelbinding.Models[Team, Project]) Project {
        return previous.Child
    },
    ProjectRelations().Tasks, TaskFields().Slug,
    func(p Path) Slug { return p.Task },
)
bound := modelbinding.Bind(endpoint, tasks)
```

The result is `Models[Models[Team, Project], Task]`. All prior models remain typed
and visible to IDE completion. A domain handler receives this value in
`modelbinding.Input[P,Q,B,M].Model` alongside the prepared transport `Request`.
For a custom child lookup, `Then(parent, func(ctx, path, loadedParent)
(value.Optional[Child], error))` provides the same owned, short-circuiting chain.
There is no global model registry or cross-request result cache.

## Authorization order and transactions

Configure request-only `WithAuthorization` on the original HTTP endpoint first;
it runs after preparation/validation and before any model lookup. Configure
`bound.WithAuthorization` for a resource policy. It receives the complete loaded
bundle, and for `BindAuthenticated`, the concrete actor (or Optional actor).
Denial stops domain work. A matching relationship alone is never authorization.

Signed and authenticated endpoints keep their transport descriptors and URL
generation. The persistence models are not exported into the request/response
schema: return an explicit DTO. Panic, Goexit and cancellation retain callback
ownership; a canceled or failed chain cannot publish partially resolved models.

Pass the caller's `database.Executor`, including a `*database.Tx` when needed.
These helpers open no transaction and take no locks. Locked queries retain their
transaction-only API and cannot be passed as ordinary model scopes. A handler
making ownership-sensitive writes should recheck or lock inside its transaction;
an earlier read is not a concurrency guarantee. Configured eager loads may query
additional records, independently of the one parent lookup per chain level.

T04 tests cover native PostgreSQL, duplicate/scoped keys, deeper relations,
soft deletion, policy ordering, hooks, metadata, compiler rejection, IDE support
and owned cancellation/concurrency. The [acceptance evidence](../evidence/typed-api-t04.json) records the passed
checks, source hashes and observed query counts.
