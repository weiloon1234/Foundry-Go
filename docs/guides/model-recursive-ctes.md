# Typed recursive common table expressions

<!-- compiled-examples -->

`rows.RecursiveCTE` and `rows.RecursiveAllCTE` combine an anchor record source with
a step callback. The callback receives `rows.RecursiveSelf[R]` and must return the
same complete record type. The ordinary struct and rows/model declaration own
column order, nullability and codecs; no model generator is involved.

A step can join children to the current self input:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go -->
```go
func descendants(self rowset.RecursiveSelf[models.User]) rowset.RecordSource[models.User] {
	parent := rowset.AsRecord[parentAlias](self, "parent")
	child := rowset.As[childAlias](models.Users.Query(), "child")
	p, c := rowset.Scope(parent), rowset.Scope(child)
	joined := rowset.InnerJoin(child, parent, rowset.OnEqual(expr.At(c, models.Users.IntroducerID), expr.NullableRow(expr.At(p, models.Users.ID))))
	return rowset.SelectRecord(joined, rowset.LeftScope(joined, rowset.Scope(child)))
}
```

The `rowset` import is `database/orm/rows`; alias tags are declared in the linked
package. The [complete consumer](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go)
shows anchors, construction, derived sources and execution.

## Recursive scope and termination

The step callback runs once, synchronously, while constructing the declaration;
it is not a per-row callback. Panics have ordinary Go behavior. The anchor's rows
are included in the result.

Use `rows.AsRecord` to alias self only inside the owning recursive step. The step
must reference its own working table exactly once; the anchor cannot reference it.
Self cannot escape as an independently executable source or be captured by a
separate CTE. Invalid anchors, step shapes and ownership fail before executor access.

Self is rejected in expression subqueries, nullable outer-join sides,
INTERSECT ALL, either side of EXCEPT ALL and the right side of EXCEPT. A derived
FROM source may retain a valid self-reference. Aggregates are rejected at a
SELECT level that directly references self. `Materialized()` is supported;
`NotMaterialized()` is rejected for a recursive definition.

`RecursiveCTE` uses UNION deduplication of complete rows; `RecursiveAllCTE` retains
duplicates. Neither provides automatic graph cycle detection. A row carrying a
changing depth/path can remain distinct forever. Bound the traversal with a domain
condition and a cancellation deadline. Put pruning conditions inside the step;
a consuming filter only filters returned rows. Order the consuming query explicitly:
evaluation order is not an output-order contract. Input limits do not limit
recursion depth. Outer LIMIT is not a general termination guarantee when joins,
sorts or aggregation need the full result. Compiler limits bound query structure,
not traversal depth or iterations; there are no implicit SEARCH/CYCLE clauses.

`rows.EachRead` closes rows after a callback error, but driver cleanup may drain
remaining results. To interrupt potentially endless recursion promptly, **cancel
a child query context before returning the callback error**. Keep an overall
deadline too. The retained
[cancellation example](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go)
in `TestPostgresNaturalKeyRecursionAndCancellation` demonstrates that order.
An ordinary callback error does not cancel the caller's shared context or silently
roll back a caller-owned transaction. Cancellation can invalidate the transaction
or connection; the caller still owns recovery and completion under the
[database runtime contracts](database-runtime.md).

The descriptor can be reused in joins, projections, scalar subqueries and other
CTEs. Inner parameters and definitions use statement-wide numbering/discovery;
independent SELECT scopes remain isolated. The read runtime keeps normal codec,
NULL, collection-disposal and streaming contracts.

Both recursive constructors accept ordinary `rows.RecordSource[R]` anchors and
steps. There is no transaction-required recursive constructor: locked or
transaction-required sources cannot be recursive inputs. An ordinary completed
recursive result may execute on a caller-owned transaction, or be lifted with
`rows.TransactionOf` for transaction-required composition. That lift does not
add locks to the recursive body or authorize transaction-required anchors/steps.

Go record compatibility does not prove physical SQL types, typmods or collations;
PostgreSQL still checks them. For nonrecursive composition see [CTEs](model-ctes.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
