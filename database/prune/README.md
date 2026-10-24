# Bounded model pruning

Register handwritten ORM selections with `ORMModel` for ordinary models or
`ORMSoftModel` for soft-delete models:

```go
registry, err := prune.New(
    prune.ORMModel("app.logs", Logs.Query().Where(Logs.Expired.Eq(true)), prune.Lifecycle),
    prune.ORMSoftModel("app.users", Users.Query().OnlyTrashed(), Users.Soft, prune.Mass),
)
if err != nil {
    return err
}
result, err := registry.Run(ctx, db, prune.DefaultOptions())
```

Each batch is atomic and bounded by `orm.MaxPerModelWriteRows`. Selection preserves
scopes, filters and visibility, locks candidates in primary-key order, and closes
rows before callbacks. `Lifecycle` invokes each model's deletion lifecycle;
`Mass` acknowledges skipping hooks, observers and audit. Soft models require the
token from the exact table declaration. A failed batch contributes no confirmed
removals; earlier committed batches remain in `Result.Counts`. Unknown transaction
outcomes remain errors and are never retried automatically.

The generated-query `Model` adapter remains available for compatibility. New
registrations use the ORM functions above; the registry and its bounds do not
depend on the legacy query engine. Legacy `Model` is isolated in `legacy.go` until
its public entry point can be retired in a coordinated change.
