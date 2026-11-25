# Alternate keys and nested model binding

The [independent consumer](../../tests/fixtures/consumer/nestedbindings/bindings.go)
contains complete typed declarations and a three-level handler.

## Bind declared keys and relationships

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database`, `database/orm`, `http/modelbinding`, `model`.
The application owns the `Team`, `Project` and `Task` models, their generated
`TeamRecords`, `ProjectRecords`, `TaskRecords`, `TeamLinks` and `ProjectLinks`,
and the string type `Slug`. The request path and the bound result types are:

<!-- compiled-source: tests/fixtures/consumer/nestedbindings/bindings.go#scoped-model-binding-path -->
```go
//foundry:path pattern=/teams/{team}/projects/{project}/tasks/{task}
type Path struct {
	Team    model.ID[Team]
	Project Slug
	Task    Slug
}
```

These aliases name the resolved models of a team and its project, and of that pair and a task:

<!-- compiled-source: tests/fixtures/consumer/nestedbindings/bindings.go#scoped-model-binding-types -->
```go
type TeamProject = modelbinding.Models[Team, Project]
type Resources = modelbinding.Models[TeamProject, Task]
```

Resolve a team and one of its enabled projects:

<!-- compiled-source: tests/fixtures/consumer/nestedbindings/bindings.go#scoped-model-binding-projects -->
```go
func Projects(db database.Executor) modelbinding.Resolver[Path, TeamProject] {
	teams := modelbinding.ByKey(db, orm.Keyed(TeamRecords.Query(), TeamRecords.Key()), func(path Path) model.ID[Team] { return path.Team })
	return modelbinding.ThroughRelation(db, teams, TeamLinks.Projects.Where(orm.Eq(ProjectRecords.Enabled, true)), ProjectRecords.Slug, func(path Path) Slug { return path.Project })
}
```

The resolver returns `modelbinding.Models[Team, Project]` with concrete `Parent`
and `Child` fields. The team resolves once. Project lookup adds the relationship's
stored parent key to its existing scope; there is no global project fallback.
Two teams may have the same project slug. Missing parent/child returns 404; SQL,
codec and retrieval-hook failures remain errors and publish no partial bundle.

For an alternate key without a parent, use
`modelbinding.ByColumn(db, ProjectRecords.Query(), ProjectRecords.Slug, selectSlug)`.
The stored field preserves both model owner and key type. Duplicate keys in the
selected scope are an internal configuration/data error. The shared
`orm.Unique`/`orm.RelatedUnique` owner checks at most two rows in one SELECT,
avoiding a separate count/find race. Database unique constraints are still
recommended. Limit/offset scopes reject because they could hide duplicates.

`ThroughRelation` accepts direct ORM relationships: BelongsTo, HasOne and HasMany.
Their filters, soft deletion, eager loading and retrieval hooks remain active.
It does not reinterpret a many-to-many pivot as a direct foreign key. Use `Then`
with an explicit typed custom resolver when the domain relationship is different.

## Add another level

`Resolve` adds the task level to the resolver:

<!-- compiled-source: tests/fixtures/consumer/nestedbindings/bindings.go#scoped-model-binding-resolve -->
```go
func Resolve(db database.Executor) modelbinding.Resolver[Path, Resources] {
	teamsEndpoint := Projects(db)

	return modelbinding.ThroughSelectedRelation(db, teamsEndpoint, func(previous TeamProject) Project { return previous.Child }, ProjectLinks.Tasks, TaskRecords.Slug, func(path Path) Slug { return path.Task })
}
```

The result is `Models[Models[Team, Project], Task]`. All prior models remain typed
and visible to IDE completion. A domain handler receives this value in
`modelbinding.Input[P,Q,B,M].Model` alongside the prepared transport `Request`.
For a custom child lookup, `Then(parent, func(ctx, path, loadedParent)
(value.Optional[Child], error))` provides the same owned, short-circuiting chain.
There is no global model registry or cross-request result cache.

## Authorization order and transactions

Configure request-only `http.AuthorizeEndpoint` on the original HTTP endpoint first;
it runs before the binding stage. Resource authorization follows resolution,
before ordinary body validation. Configure
`modelbinding.AuthorizeResource(bound, policy)` for a resource policy. It receives the complete loaded
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

Tests cover native PostgreSQL, duplicate/scoped keys, deeper relations,
soft deletion, policy ordering, hooks, metadata, compiler rejection, IDE support
and owned cancellation/concurrency.
