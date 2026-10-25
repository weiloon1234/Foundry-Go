# Conflict policies

`upsertqueries` uses the shared `models.Users` and `models.WriteRecords`
declarations for inserts and typed `orm.Upsert`/`orm.UpsertMany` policies.
`ExcludedRecords` declares the physical table `excluded` and exercises its
stored/proposed alias disambiguation through `rows.ConflictRowsOf`.

The PostgreSQL cases retain defaults, explicit zero/null values, composite and
named conflict targets, partial/expression indexes, trigger-modified proposed
values, scalar and correlated calculations, batch rollback and concurrent identity
assertions. Reusing the caller's assignments with `orm.Compare` checks that UUID
synthesis leaves the caller's key omitted. Invalid and empty batches assert that
no transaction opens; the retained `query.MaxInsertRows` compatibility constant
supplies the shared insert bound without using the legacy query engine.

PostgreSQL tests remain opt-in through the existing fixture helper. The model
generation directive is removed; scenarios and compiler negatives use the ORM.
