# Joins and model scopes

`As[Tag](query, alias)` names one model occurrence. Use distinct tag types and SQL
names for each occurrence, including self joins. Aliases preserve filters, order,
limits, offsets and lazy model scopes inside their original SELECT boundary.

`InnerJoin`, `LeftJoin`, `RightJoin` and `FullJoin` take `On(left, right)` with
matching concrete key/value types and the exact left and right input scopes.
`CrossJoin` has no ON condition. A join chain goes on the left; the right input
is one alias. Duplicate alias names or tag identities fail before execution.
Each join kind has a distinct scope type.

```go
u := rows.As[userSide](Users.Query(), "u")
o := rows.As[orderSide](Orders.Query(), "o")
j := rows.LeftJoin(u, o, rows.On(
    expr.At(rows.Scope(u), Users.ID),
    expr.At(rows.Scope(o), Orders.BuyerID),
))
userEmail := expr.At(rows.LeftScope(j, rows.Scope(u)), Users.Email)
orderID := expr.AtNullable(rows.NullableRightScope(j, rows.Scope(o)), Orders.ID)
// Map orderID to a declared value.Nullable[model.ID[Order]] row field.
```

`LeftScope` and `RightScope` preserve non-nullable sides. Left joins require
`NullableRightScope`; right joins require `NullableLeftScope`; full joins require
both. `expr.AtNullable` widens a required column. `expr.AtNullableValue` keeps one
nullable layer for an already nullable column. Both retain present-value
comparisons and nullable selected results. `expr.At` accepts only preserved scopes.
Use `LeftNullableScope(chain, nullableScope)` to retain an earlier nullable side
through another join. Nullable scopes cannot convert to preserved scopes.

`rows.Select` maps complete declared rows. Its mappings, predicates and orders
must use the resulting join scope. References to another declaration with the
same Go model or another alias with the same tag fail validation before SQL.
A NULL from an outer join decodes as `value.Null`, never a Go zero value.

Subqueries, ON condition composition, correlation, LATERAL and transaction-only
sources remain later Stage 6b slices. Their existing `database/query` APIs remain
available. PostgreSQL FULL JOIN conditions must satisfy its planner restrictions;
no implicit condition rewrite changes those semantics.
