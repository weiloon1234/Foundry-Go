# Computed query keys

<!-- compiled-examples -->

`expr.Group(rowValue)` constructs a row grouping key. A selected expression's
`.Key()` constructs an `expr.ProjectionKey[S]` for selected DISTINCT ON and window
partitioning. Neither operation erases scope or makes an aggregate a row operand.

## Group by a computed value

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/keyqueries/keys_test.go):

<!-- go-source: tests/fixtures/consumer/keyqueries/keys_test.go -->
```go
func buckets() rowset.Query[models.User, keyqueries.BucketCount] {
	u := models.Users
	bucket := expr.When(expr.ModelPredicate(u.Age.Gt(20)), expr.Param(u.Age, 1)).Else(expr.Param(u.Age, 0))
	return rowset.OrderBy(rowset.GroupBy(rowset.Select(rowset.From(models.Users.Query()), keyqueries.BucketCounts.Row,
		rowset.MapSelected(keyqueries.BucketCounts.Bucket, bucket.Value()),
		rowset.MapSelected(keyqueries.BucketCounts.Count, expr.Count[models.User]().Value()),
	), expr.Group(bucket)), bucket.Asc())
}
```

The `rowset` import above names `database/orm/rows`. The handwritten
[BucketCounts schema](../../tests/fixtures/consumer/keyqueries/rows.go) maps the
ordinary result struct. `rows.GroupBy` appends keys. Duplicate/zero keys and
ungrouped selections fail compilation. Grouping by a CASE result does not make
its input columns independently selectable; compiler validation does not infer
functional dependencies from primary keys.

## Selected keys and clauses

`rows.DistinctOn` accepts row keys; `rows.DistinctOnValues` accepts selected keys.
Scalar equivalents are `GroupByScalar`, `HavingScalar`, `DistinctScalar`,
`DistinctOnScalar` and `DistinctOnScalarValues`. Correlated scalar equivalents
retain the required outer owner. `DistinctOnValueSet` uses the combined set's
selected keys. See [distinct reads](model-distinct.md) for ordering-prefix checks.

`Window.PartitionBy` takes row keys and `PartitionByValues` takes selected keys.
Both append to the same window. Duplicate keys across the two methods fail.
An aggregate partition key establishes grouping; a window partition cannot
contain another window at the same SELECT level.

## Identity and scope boundaries

Key matching uses canonical SQL and encoded parameter values, reusing parameter
positions within a SELECT. Different values are different keys. No algebraic or
collation equivalence is inferred. Integer parameters remain values, not ordinal
references. SQL groups NULL keys together. Custom codecs must encode equivalent
parameter values consistently for key matching.

Grouping a computed key does not permit a nested subquery to capture that key's
ungrouped raw inputs. Project and alias the grouped result before reusing its
output as a row field.

For [result cursors](result-cursor-pagination.md), project the computed value into
a handwritten rows schema, then name its output field in `UniqueBy`. A computed
group key is not a declared cursor identity. `rows.CompileCursor` and
`rows.PreflightCursor` validate cursor keys without I/O.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
