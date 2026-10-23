# Scoped expressions

Use `expr.Value(column)` for a model scope and `expr.At(rows.Scope(alias), column)`
for an alias. Existing `Expression[S,V,P]` distinguishes the present comparison
value `V` from the projected value `P`, including `value.Nullable[V]`.

`RowValue[S,V]` accepts stored and computed row values. `Selected[S,V]` accepts
selected results and cannot enter row predicates or row arithmetic. Promote row
values with `expr.Select`, or use a computed value's `Value` method. Use
`rows.MapSelected` to select an aggregate or a computed result into a declared row
field. `rows.Map` retains its existing column-expression signature.

```go
age := expr.Value(Users.Age)
q := expr.Where(Users.Query(), expr.Add(age, expr.Param(Users.Age, 2)).Gt(18))
q = expr.OrderBy(q, expr.Negate(age).Asc())
```

Arithmetic preserves the concrete numeric type. Integer division truncates toward
zero; database overflow and division-by-zero remain errors. Remainder excludes
floating-point inputs. `DecimalOf` accepts exact integers/decimals and returns
`decimal.Decimal`; `FloatOf` explicitly returns approximate `float64`.

Nullable arithmetic requires nullable operands: explicitly widen required values
with `NullableRow`. Nullable calculations keep one nullable result layer; their
literal comparisons take present values. Bound parameters use a column's existing
codec and are captured when constructed; zero, false and empty values remain supplied
values. `Param` on a nullable column takes the present value; `NullFor` constructs
an explicitly typed SQL NULL. Computed/selected `Param` methods take their exact
result type.

`Equal`, ordered comparisons, `Like`, `DistinctFrom` and their selected `*Value`
counterparts compare expressions with the same owner and concrete type. Selected
comparisons return `HavingPredicate`, never a row predicate. `OrderRow` and
`OrderValue` expose literal range comparisons only on ordered types. Stored enum
handles do not satisfy `GreaterColumn`'s ordered-column capability.

`Count[S]()` counts rows, including NULL fields, returning required `int64` with
zero for empty input. `Exists[S]()` returns required `bool`, false for empty input,
and exposes no range comparisons. Both use the existing aggregate compiler and
preserve the source query's scopes, executor and transaction.

Aggregate summaries, GROUP BY/HAVING, DISTINCT and raw fragments belong to later
Stage 6a slices. Specialized text and temporal functions belong to Stage 6d.

Row and selected phases have different private marker fields, so explicit Go
conversions cannot move selected aggregates or HAVING conditions into row WHERE.
Internal bridges validate family-specific pointer-backed capabilities; a zero value
synthesized through generic inference is rejected before metadata is exposed or
applied. The framework passes issued capabilities only to verified concrete owners,
so application wrappers cannot capture one through an inferred method signature.
Reference-free parameters retain their construction scope and declaration owner.
An invalid exemplar stays invalid through `Param`, including scoped ownership errors.
The ownership core merges references as a bounded, stable, deduplicated set, so
reusing a calculation does not multiply its retained references.

Use `Coalesce(nullable, requiredFallback)` to remove one nullable layer. Zero and
empty values remain supplied values. `CoalesceNullable` accepts only nullable
inputs and retains one layer. `NullIf(a, b)` requires matching required inputs and
adds one nullable layer; use `NullIfNullable` for inputs already nullable.
The `*Value` variants accept `Selected` values and retain the selected phase.

```go
nickname := expr.Value(Users.Nickname)
email := expr.Value(Users.Email)
label := expr.When(nickname.IsNull(), email).
    When(email.Eq(""), expr.Param(Users.Email, "anonymous")).
    Else(expr.Coalesce(nickname, email))
```

`When` returns unfinished `Case[S,V]`. Append matching branches with `When`, then
finish with `Else` or `ElseNull` before selecting or comparing it. `Else` preserves
the exact branch type, including nullability. `ElseNull` adds one nullable layer to
required branches; nullable branches need a nullable `Else` instead. Each branch
addition leaves existing CASE values unchanged.

`WhenValue` returns unfinished `ValueCase[S,V]`, takes `HavingPredicate[S]` and
selected branch values, and finishes as `Selected`. For example:
`expr.WhenValue(count.Gt(0), count.Value()).Else(count.Param(0).Value())`.
`Grouped` explicitly lifts a row predicate for selected CASE; the shared compiler
still requires its columns to be grouped when aggregates are present. Group keys
and query HAVING entry points arrive in the next slice. All branches and fallbacks
retain source ownership, codecs and errors through subsequent composition/`Param`.
