# Hook query fixture

`Members` and `Logs` declare the stored models. `Members` installs one
`orm.Hooks[Member]` factory per write. Its callbacks use `*orm.Write[Member]` and
persisted `orm.Changes[Member]`; `Trace.ORMChanges` records those snapshots.

The ORM tests retain defaults, omitted/null/value inputs, transactional log writes,
locked originals, hook vetoes, nested savepoints and after-commit ordering. Local
and observer after-commit errors retain identity without invoking formatting.
Generated declarations remain only for the integration barrier.
