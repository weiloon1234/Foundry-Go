# cursorqueries consumer

Projection, completed-record composition and recursive result cursors use ORM/rows declarations. Query identity, one-statement page reads, cancellation, nullable codec failures, bounds and partial-page disposal retain their assertions.

Grouped results, DTO DISTINCT ON, CTE/set windows and nullable/window scalar
pagination now use ORM/rows. `legacy_clauses_postgres_test.go` retains only the
joined complete-model DISTINCT ON assertion: ordinary `rows.RecordRead` still
lacks a DISTINCT ON adapter that keeps input-scope ordering. Lane C owns this
remaining clause gap. The complete model is not replaced with a DTO decoder.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
