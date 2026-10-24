# Transaction composition

`Records` owns the local model declaration. `Summaries`, `TotalRows` and `Pairs`
map the existing result structs without generated projection builders.
`Claimed`, `Joined` and `CountClaimed` retain their exported signatures.

Model locks enter row composition through `rows.LockedRecord`; ordinary records
require `rows.TransactionOf(rows.ModelRecord(...))`. CTEs, joins, scalar queries,
correlations and LATERAL sources keep that transaction requirement. Scoped fields
are bound through `expr.At`, and nullable joins use `expr.AtNullable` or
`rows.AtNullable`. Lock targets use `rows.Target` on preserved scopes only.

The PostgreSQL tests retain independent-transaction lock checks, SKIP LOCKED,
savepoint rollback, cancellation, rejected aggregate/nullable locks, scalar
cardinality errors, SQL three-valued membership and nested correlation ownership.
The compiler cases exercise these boundaries against the local ORM declarations.
Generated declarations remain until the lead's generation barrier; legacy SQL
calls in the declaration golden remain as independent oracles.
