# pagequeries consumer

Ordinary model pagination, eager lookahead and projected pages use ORM/rows declarations. Counts, lookahead hydration, eager-query accounting, cancellation and failure disposal retain their assertions.

Grouped results, DTO DISTINCT ON, CTE/set windows and nullable/window scalar
pagination use ORM/rows. `record_distinct_postgres_test.go` keeps the joined
complete-model DISTINCT ON assertion through `rows.DistinctOnRecord`, preserving
input-scope ordering and the complete model decoder.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
