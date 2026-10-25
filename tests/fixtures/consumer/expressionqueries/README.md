# Conditional expression acceptance

The PostgreSQL consumers use ORM row/selected CASE, COALESCE and NULLIF with scalar
grouping and DISTINCT. They preserve enum validation, nullable results, conditional
window input grouping, filtered aggregates, CTE/correlated composition, relation
aggregate slots and model-write predicates.

The direct scalar Compile assertions in `TestTypedConditionalSQL` remain on the
legacy API until lane A's compile terminal is integrated. No assertion is skipped.
