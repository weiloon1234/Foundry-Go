# pagequeries consumer

Ordinary model pagination, eager lookahead and projected pages use ORM/rows declarations. Counts, lookahead hydration, eager-query accounting, cancellation and failure disposal retain their assertions.

`legacy_clauses_postgres_test.go` preserves grouped, joined, CTE/set, DISTINCT ON, scalar DISTINCT and window page assertions. The combined test remains blocked on lane C's ordinary-record DISTINCT ON and scalar DISTINCT adapters.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
