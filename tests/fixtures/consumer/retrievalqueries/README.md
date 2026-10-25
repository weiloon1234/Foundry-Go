# Retrieval query fixture

`Members`, `Groups` and `Memberships` publish complete ORM model declarations;
`MemberLinks` owns their relations. `MemberLabels` declares the separate DTO row.
The migrated application registers ORM retrieval observers after the local factory.

Tests cover hydration before callbacks, closed rows, per-batch factories, bounded
streaming, complete records through joins/CTEs/unions, locked reads and cursors.
Scalar and DTO reads skip model retrieval callbacks. Model accessors remain explicit.
The shared default chunk-size constant remains a legacy compatibility alias;
queries no longer use the generated engine. Generated output stays until the barrier.

Complete-record streaming remains blocked: `rows.RecordRead` has no `Each` API, and
`rows.Each` accepts DTO queries only. The original CTE iteration oracle remains in
`legacy_iteration_postgres_test.go` using a separate application with the legacy
member observer family. The ORM model-streaming test retains its closed-batch and
stop-early assertions. No application registers both families for one model.
