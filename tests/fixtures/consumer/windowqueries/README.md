# Window query acceptance

`Rankings`, `Navigation` and `WindowTotals` are handwritten rows schemas for the
unchanged report structs. Tests use `expr.WindowFor`, explicit grouping keys and
`expr.Over`, with ORM model declarations and scopes. They preserve ranking,
frame/exclusion, navigation/fallback, imported enum, recursive, correlation,
streaming and cancellation assertions. Projection directives remain until the
lead's generated-output cleanup barrier.
