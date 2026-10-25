# pagequeries consumer

Ordinary model pagination, eager lookahead and projected pages use ORM/rows declarations. Counts, lookahead hydration, eager-query accounting, cancellation and failure disposal retain their assertions.

Grouped results, DTO DISTINCT ON, CTE/set windows and nullable/window scalar
pagination now use ORM/rows. `legacy_clauses_postgres_test.go` retains only the
joined complete-model DISTINCT ON assertion: ordinary `rows.RecordRead` still
lacks a DISTINCT ON adapter that keeps input-scope ordering. Lane C owns this
remaining clause gap. The complete model is not replaced with a DTO decoder.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
