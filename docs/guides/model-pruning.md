# Model pruning

`database/prune` removes obsolete models in bounded batches, like Laravel's `Prunable` and `MassPrunable` models. Declare each prunable model with its typed selection and run the registry explicitly, typically from a scheduled command. Nothing prunes during application boot. The [tenant consumer](../../tests/fixtures/consumer/tenantqueries/prune_postgres_test.go) exercises both modes and the command.

```go
registry, err := prune.New(
    prune.Model("app.trashed_documents",
        QueryDocuments().WithoutGlobalScopes().OnlyTrashed().Query, prune.Mass),
    prune.Model("app.expired_sessions",
        QuerySessions().Where(SessionFields().ExpiresAt.Lt(cutoff)).Query, prune.Lifecycle),
)
result, err := registry.Run(ctx, db, prune.DefaultOptions())
```

The selection is an ordinary model query, so its filters, [global scopes](model-global-scopes.md) and soft-delete visibility decide what is obsolete; remove scopes explicitly when a maintenance job must see every tenant. A soft-delete model is force-deleted within the selection's visibility, so select trashed models with `OnlyTrashed` or `WithTrashed`.

`prune.Lifecycle` removes each model through its ordinary (force) delete lifecycle, running hooks and provider observers. `prune.Mass` removes each batch with one `DELETE` and no per-model hooks; choosing it is the acknowledgement that hooks are skipped. Each batch is one transaction that locks at most `BatchSize` candidates in primary-key order (`Query.PruneBatch` is the execution boundary). A run repeats batches until one removes fewer than `BatchSize` models or `MaxBatches` is reached, in which case `Count.Remaining` is true. `DefaultOptions` allows 100 batches of 500 models per declaration. A failure stops the run; `Result` reports the committed removals and `StoppedAt`, and committed batches stay removed.

## Command

Consumer binaries expose the registry through the [database commands](migrations-and-seeding.md#consumer-commands) by setting `command.Resources.Prunables`:

```text
prune list [--format text|json]
prune run [--format text|json] [--name app.sessions ...] [--batch-size N] [--max-batches N]
```

`prune run` without `--name` runs every declaration in name order.

## Scheduled pruning

Configured applications can run declarations on the scheduler leader through the
[housekeeping schedule](production-operations.md#housekeeping-schedule). Return
`application.PruneModels(services, connection, declare)` in
`FeatureDeclarations.Pruning`; `declare(now)` runs at the start of every run with
the application clock, so cutoffs stay relative to that run:

```go
models, err := application.PruneModels(services, "", func(now time.Time) ([]prune.Target, error) {
    cutoff, err := temporal.NewDateTime(now.Add(-30 * 24 * time.Hour))
    return []prune.Target{prune.Model("app.expired_sessions",
        QuerySessions().Where(SessionFields().ExpiresAt.Lt(cutoff)).Query, prune.Lifecycle)}, err
})
```

The task `models.<connection>` uses `features.maintenance.models` batch settings
(`batch` as `BatchSize`, `max_batches` as `MaxBatches`).
