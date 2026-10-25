# Observer query fixture

`Records`, `Plains` and `Effects` publish ORM declarations. `Records` attaches the
local per-write factory. `Plains` deliberately has no local factory, exercising
registered observers on an otherwise unannotated model. `Effects` writes through
the current transaction.

For an ORM application, register `orm.NewObserver[Record]` with a factory returning
`ORMObserverHooks(name)`. Register `orm.NewObserver[Plain]` independently when its
default is needed. Retrieval registrations use `orm.NewRetrievalObserver`.
The application setup registers only the ORM family. Registration rejects a
foreign hook family without constructing either factory.

`Trace.ORMChanges` contains persisted changes. Ordering, rollback, cancellation,
concurrent-operation and row-lifetime assertions now exercise ORM operations.
`lifecycle.CountObservers[Record](rows.Observers())` checks registration counts after
row closure without accessing private adapters or constructing factories.
Legacy hook state and model-generation directives are removed; event integration
uses the same ORM observer family.
