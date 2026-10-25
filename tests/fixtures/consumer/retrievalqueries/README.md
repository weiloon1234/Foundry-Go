# Retrieval query fixture

`Members`, `Groups` and `Memberships` publish complete ORM model declarations;
`MemberLinks` owns their relations. `MemberLabels` declares the separate DTO row.
The migrated application registers ORM retrieval observers after the local factory.

Tests cover hydration before callbacks, closed rows, per-batch factories, bounded
streaming, complete records through joins/CTEs/unions, locked reads and cursors.
Scalar and DTO reads skip model retrieval callbacks. Model accessors remain explicit.
The shared default chunk-size constant remains a legacy compatibility alias;
queries no longer use the generated engine. Generated output stays until the barrier.

Complete-record CTE streaming uses `rows.EachRead`. Its offset/limit window,
closed bounded batches, per-batch factory counts, nested SQL during delivery and
error identity on early stop retain the original assertions. All retrieval tests
now use one ORM observer family; the temporary legacy registration bridge is gone.
