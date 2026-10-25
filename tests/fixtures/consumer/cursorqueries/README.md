# cursorqueries consumer

Projection, completed-record composition and recursive result cursors use ORM/rows declarations. Query identity, one-statement page reads, cancellation, nullable codec failures, bounds and partial-page disposal retain their assertions.

`legacy_clauses_postgres_test.go` preserves grouped/joined DISTINCT ON and scalar DISTINCT/window cursor assertions. Their full cutover needs lane C's ordinary-record DISTINCT ON and scalar DISTINCT adapters. No duplicate ORM decoder or raw-SQL substitute is introduced.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
