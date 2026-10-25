# Lateral query acceptance

`UserOrders` and `RankedOrders` are handwritten rows schemas for the unchanged
result structs. Ordinary correlated records use `rows.HavingCorrelatedRecord`
and `rows.DistinctCorrelatedRecord` before `AsLateral`; they retain their parent
scope and cannot become independent record sources or execute on their own.

The migrated tests preserve per-parent limits, offsets, NULL-extension, nested
correlations, aggregate/window projections, CTE discovery, result pagination,
locks, cancellation and preflight errors. SQL parity compares correlated clauses
against the retained generated API. The lead runs the PostgreSQL assertions.

Generated declarations and outputs remain until the integration cleanup barrier.
