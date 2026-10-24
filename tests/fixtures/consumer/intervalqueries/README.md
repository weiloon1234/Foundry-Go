# Interval queries

`Samples`, `Plans`, `Labels` and `Links` are handwritten ORM declarations.
Interval keys keep their calendar/elapsed components when hydrated and retain
PostgreSQL equality for finds, conflicts, relations and cursor ties.

`PlanLinks` owns the direct and pivot relations. `PlanValues` binds count and
nullable interval sum slots. `Summaries` maps the existing `Summary` result type;
`expr.SumInterval` and `expr.AvgInterval` retain nullable interval results.
Arithmetic uses `expr` row values and explicit selected values, including filtered
window aggregates, rather than generated projection builders.

The PostgreSQL fixtures retain the previous codec-style, nullability, overflow,
partial-result, timezone, pivot, aggregation and cursor assertions. Explicit SQL
is confined to schema/session setup and deliberate invalid driver values.
Generated declarations remain for the lead's cleanup barrier and the independent
old/new SQL goldens.
