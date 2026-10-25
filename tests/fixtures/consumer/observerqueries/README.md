# Observer query fixture

`Records`, `Plains` and `Effects` publish ORM declarations. `Records` attaches the
local per-write factory. `Plains` deliberately has no local factory, exercising
registered observers on an otherwise unannotated model. `Effects` writes through
the current transaction.

For an ORM application, register `orm.NewObserver[Record]` with a factory returning
`ORMObserverHooks(name)`. Register `orm.NewObserver[Plain]` independently when its
default is needed. Retrieval registrations use `orm.NewRetrievalObserver`.
Never combine generated and ORM observer adapters for one model in one application.
The application setup registers only the ORM family.

`Trace.ORMChanges` contains persisted changes. Ordering, rollback, cancellation,
concurrent-operation and row-lifetime assertions now exercise ORM operations.
`lifecycle.CountObservers[Record](rows.Observers())` checks registration counts after
row closure without accessing private adapters or constructing factories.
`Trace.Changes` and the generated APIs remain for eventqueries and the integration
barrier; ordinary observer fixture tests use the ORM family.
