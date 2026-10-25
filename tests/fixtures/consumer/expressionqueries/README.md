# Conditional expression acceptance

The PostgreSQL consumers use ORM row/selected CASE, COALESCE and NULLIF with scalar
grouping and DISTINCT. They preserve enum validation, nullable results, conditional
window input grouping, filtered aggregates, CTE/correlated composition, relation
aggregate slots and model-write predicates.

Direct scalar SQL assertions use `rows.CompileRead` with `rows.ReadScalar`.
No assertion is skipped or replaced by a runtime-only check.
