# framequeries

Numeric and temporal RANGE frames, named windows and correlated windows use `Samples`, `expr` and `rows`. `CompileRead` and `PreflightRead` retain the compiler boundary; `sql_parity_test.go` compares named frame SQL and bindings with the legacy API. PostgreSQL assertions remain in the original test entry points.
