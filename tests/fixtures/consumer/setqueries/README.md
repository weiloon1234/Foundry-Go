# setqueries

Scalar and model set operations use the shared rows owners and read terminals, preserving multiplicity, windows, streaming cleanup and cancellation. Correlation over a complete set uses an explicit record alias. Aggregating a declared nullable projection field uses `expr.SumNullableRow`, preserving exact decimals and one nullable layer.
