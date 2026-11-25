# Computed query keys

`expr.Group(rowValue)` constructs a row grouping key. A selected expression's
`expr.Key(selected)` constructs an `expr.ProjectionKey[S]` for selected DISTINCT ON and window
partitioning. Neither operation erases scope or makes an aggregate a row operand.

## Group by a computed value

`models.User` is the application's model with an integer `Age`, and `models.Users` its generated table. `BucketCount` is the result struct with `Bucket int` and `Count int64`, and `BucketCounts` its handwritten `rows.Define` schema. `Buckets` counts users by age bucket:

<!-- compiled-source: tests/fixtures/consumer/keyqueries/guide_keys.go#computed-query-keys -->
```go
func Buckets() rows.Query[models.User, BucketCount] {
	u := models.Users
	bucket := expr.When(expr.ModelPredicate(orm.Gt(u.Age, 20)), expr.Param(u.Age, 1)).Else(expr.Param(u.Age, 0))
	return rows.OrderBy(rows.GroupBy(rows.Select(rows.From(models.Users.Query()), BucketCounts.Row,
		rows.MapSelected(BucketCounts.Bucket, expr.AsSelected(bucket)),
		rows.MapSelected(BucketCounts.Count, expr.AsSelected(expr.Count[models.User]())),
	), expr.Group(bucket)), expr.Asc(bucket))
}
```

`rows.GroupBy` appends keys. Duplicate/zero keys and
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
