# keyqueries

Computed grouping, selected DISTINCT keys, correlated scalar grouping and value-set DISTINCT ON use `expr` and thin `rows` functions. `BucketCounts` is the handwritten schema. Cursor key negatives use `rows.CompileCursor` and `rows.PreflightCursor`, preserving the before-I/O assertion boundary without adding pagination.
