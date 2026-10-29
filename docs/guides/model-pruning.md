# Model pruning

`database/prune` removes obsolete models in bounded batches. Register typed selections and run the registry explicitly; nothing prunes during application boot.

<!-- orm-example: pruning -->
```go
func pruneMembers(ctx context.Context, db database.Transactor, cutoff time.Time) (prune.Result, error) {
	f := hooks.Members
	registry, err := prune.New(prune.ORMSoftModel("app.old_members",
		f.Query().OnlyTrashed().Where(orm.TimeLt(f.UpdatedAt, cutoff)), f.Soft, prune.Lifecycle))
	if err != nil {
		return prune.Result{}, err
	}
	return registry.Run(ctx, db, prune.DefaultOptions())
}
```

`prune.ORMModel` registers an ordinary model. `prune.ORMSoftModel` additionally takes its `SoftDeletable[M]` token for physical pruning. The selection retains predicates, scopes and visibility. Remove global scopes explicitly only when the maintenance task is authorized to cross them. Select `OnlyTrashed` to remove already deleted models.

`prune.Lifecycle` invokes each model's ordinary deletion or force-deletion lifecycle. `prune.Mass` selects one SQL deletion per batch and acknowledges that hooks, observers and audit are skipped. Each batch locks candidates in primary-key order and owns a transaction. Direct `orm.PruneBatch` and `orm.ForcePruneBatch` expose the same bounded operation.

The registry repeats until fewer than `BatchSize` models are removed or `MaxBatches` is reached. In the latter case `Count.Remaining` is true. Defaults allow 100 batches of 500 models per declaration. A later failure preserves earlier committed batches; `Result` reports committed removals and `StoppedAt`. Unknown commit outcomes are not counted as confirmed removal or retried automatically.

## Command

Expose the registry through `command.Resources.Prunables` in the [database command setup](migrations-and-seeding.md#consumer-commands).

```text
prune list [--format text|json]
prune run [--format text|json] [--name app.sessions ...] [--batch-size N] [--max-batches N]
```

Without `--name`, the command runs declarations in name order.

## Scheduled pruning

Configured applications can return `application.PruneModels(services, connection, declare)` in `FeatureDeclarations.Pruning`. The `declare(now)` callback returns `[]prune.Target` with cutoffs derived from the application clock at the start of each run.

The [housekeeping schedule](production-operations.md#housekeeping-schedule) runs on the scheduler leader. Task `models.<connection>` uses `features.maintenance.models` settings: `batch` supplies `BatchSize`, and `max_batches` supplies `MaxBatches`. Registration does not authorize destructive schema changes or reset any data store.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
