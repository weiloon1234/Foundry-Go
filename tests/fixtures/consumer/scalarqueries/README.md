# scalarqueries

`Samples` declares the numeric and text columns with a typed primary role; `Calculations` declares the retained DTO fields. Runtime arithmetic, text, geography, grouping, windows, pagination and correlated expressions use ORM/rows/expr. Derived-field SUM uses `expr.SumRow` through the original CTE boundary. Computed target/pivot relation ordering uses `expr.RelationOrder` and `expr.RelationPivotOrder`, validated against the actual relation metadata.
