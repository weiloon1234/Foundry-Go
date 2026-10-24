# Observer query fixture

`Records`, `Plains` and `Effects` publish ORM declarations. `Records` attaches the
local per-write factory. `Plains` deliberately has no local factory, exercising
registered observers on an otherwise unannotated model. `Effects` writes through
the current transaction.

For an ORM application, register `orm.NewObserver[Record]` with a factory returning
`ORMObserverHooks(name)`. Register `orm.NewObserver[Plain]` independently when its
default is needed. Retrieval registrations use `orm.NewRetrievalObserver`.
Never combine generated and ORM observer adapters for one model in one application.
The test setup selects one complete family before application startup.

`Trace.ORMChanges` contains persisted ORM changes; `Trace.Changes` remains for the
retained generated suite and eventqueries dependents. Existing observer ordering,
cancellation, savepoint and concurrent-operation assertions remain in that suite;
the ORM publication tests cover registration, order, capture and rollback.
