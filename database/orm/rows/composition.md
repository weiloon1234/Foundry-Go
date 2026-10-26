# CTEs, recursion and set operations

CTEs retain a complete model or declared projection. Adapt ordinary model queries
with `ModelRecord` and projected queries with `RecordOf`, then introduce a typed
alias for each reference:

```go
eligible := rows.CTE("eligible", rows.ModelRecord(Users.Query().Where(Users.Age.Gte(18))))
a := rows.AsRecord[adult](eligible.Materialized(), "adult")
users, err := rows.SelectRecord(a, rows.Scope(a)).All(ctx, db)
```

Dependencies are emitted before their consumers, and references to the same
immutable definition share one declaration. Conflicting names, physical-table
shadowing, invalid names and cyclic dependencies fail before SQL. `Materialized`
and `NotMaterialized` return derived definitions without changing captured ones.
Mixing differently hinted definitions under one name is a conflict.

`TransactionCTE` accepts only transaction records, including `LockedRecord` and
explicit `TransactionOf` lifts. Use `AsTransaction`, `TransactionScope` and the
transaction selection functions. Locks and wait policies stay inside the CTE;
its results require the caller's `*database.Tx`.

`RecursiveCTE` uses UNION; `RecursiveAllCTE` uses UNION ALL. The callback builds the
step once synchronously and returns a complete record matching the anchor:

```go
step := func(self rows.RecursiveSelf[Node]) rows.RecordSource[Node] {
    parent := rows.AsRecord[parentTag](self, "parent")
    child := rows.As[childTag](Nodes.Query(), "child")
    joined := rows.InnerJoin(child, parent, rows.On(
        expr.At(rows.Scope(child), Nodes.ParentID),
        expr.At(rows.Scope(parent), Nodes.ID),
    ))
    return rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(child)))
}
tree := rows.RecursiveCTE("tree", rows.ModelRecord(Nodes.Query().Where(Nodes.ID.Eq(root))), step)
```

The shared compiler requires exactly one self reference in the owning step. It
rejects escaped references, nested independent CTE capture, invalid nullable join
placement, aggregates over self and other PostgreSQL recursion restrictions.
Self has no execution or mutation methods. Recursive definitions cannot request
`NotMaterialized`. UNION removes duplicate complete rows; UNION ALL retains paths.
The application must ensure termination and execute with cancellation/deadlines;
an outer LIMIT is not a general recursion bound.

`Union`, `UnionAll`, `Intersect`, `IntersectAll`, `Except` and `ExceptAll` combine
complete records of the same Go type and ordered column/nullability layout. The
result's `Scope()` is separate from each input. Input filters, ordering and windows
stay inside the set operands; result Where/OrderBy/Limit/Offset apply afterward:

```go
combined := rows.Union(rows.ModelRecord(first), rows.ModelRecord(second))
id := expr.At(combined.Scope(), Users.ID)
users, err := combined.OrderBy(id.Asc()).Limit(20).All(ctx, db)
```

Declared projection fields use `rows.At(combined.Scope(), Fields.Name)`. Sets also
support projection, aliasing and CTE composition. Model output dispatches the
existing retrieval callbacks after complete collection; projection/scalar output
does not dispatch model callbacks.

For scalars, adapt `SelectScalar` using `Values`, then use the corresponding
`UnionValues`, `UnionAllValues`, `IntersectValues`, `IntersectAllValues`,
`ExceptValues` or `ExceptAllValues`. Inputs must have exactly the same value type,
including model-owned identifiers and nullability:

```go
ids := rows.Values(rows.SelectScalar(rows.From(Users.Query()), expr.Row(expr.Value(Users.ID)).Value()))
combined := rows.UnionValues(ids, ids)
values, err := combined.OrderBy(combined.Value().Asc()).All(ctx, db)
inner := rows.SelectScalar(combined, expr.Row(combined.Value()).Value())
condition := rows.InQuery(expr.Value(Users.ID), inner)
```

A scalar set also provides its complete single-column record for CTE composition.
Ordinary record sets cannot be passed to scalar subqueries without selecting one
value explicitly. Transaction inputs cannot enter ordinary sets. Result cursors
and remaining read/page/transaction terminals are the next slice.
