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

Specialized text and temporal functions belong to Stage 6d.

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
and query HAVING entry points are supplied by `rows.GroupBy` and `rows.Having`. All branches and fallbacks
retain source ownership, codecs and errors through subsequent composition/`Param`.

Use `Sum`/`Avg` for exact stored numbers: their result is
`value.Nullable[decimal.Decimal]`, including integer inputs. `SumFloat`/`AvgFloat`
return `value.Nullable[float64]`; `SumInterval`/`AvgInterval` preserve
`temporal.Interval`. `Min`/`Max` accept ordered stored fields, preserve the field's
present type and return one nullable layer. Empty or all-NULL input returns NULL
for all these summaries. Ordered field capabilities are sealed: adding a `Gt`
method to an enum wrapper does not make it eligible for summaries or extrema.
Ordered input handles have the same aggregate capabilities as their stored kinds:
number, time and interval handles, including nullable forms, retain their stored
types; text input handles support Min/Max. Aggregates never invoke write transforms.
InputEnum, InputJSON, InputColumn and InputBytes remain outside the ordered
capability. The private framework marker cannot be supplied by an external method
with the same name.
`NullableOrderedAggregate.Param(v)` accepts a present value and returns a required
row expression, for example `expr.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())`.
It preserves the result codec, scope and existing errors. `CountField` and `CountDistinctField` skip NULLs and return
required `int64`, zero on empty input. `Count` includes NULL rows; `Exists` returns
false on empty input. There is no general aggregate DISTINCT operation.

Each field aggregate also has an `At` constructor, such as
`expr.SumAt(rows.Scope(alias), Orders.TotalCents)`, retaining stored-field
capabilities and the alias's input scope. `.Filter(exprPredicate...)` appends
same-scope row conditions with AND for that aggregate only; it retains empty-input
semantics. Query WHERE filters every aggregate; HAVING filters completed groups.
`AggregateExpression[S,V]` is the sealed reusable descriptor for relation aggregate
slots: it retains native aggregate metadata, complete ownership and the typed
result codec/scanner. Selected calculations do not implement this interface.

```go
buyer := expr.Value(Orders.BuyerID)
total := expr.Sum(Orders.TotalCents)
q := rows.Select(rows.FromModel(Orders.Query()), Totals,
    rows.Map(Totals.Buyer, buyer), rows.MapSelected(Totals.Amount, total.Value()))
q = rows.GroupBy(q, expr.Group(buyer))
q = rows.Having(q, total.Gt(decimal.FromInt64(100)))
q = rows.OrderBy(q, total.Desc(), buyer.Asc())
```

`rows.FromModel` exposes complete models through the read-only rows boundary and
can also supply a projection source. Existing model/row codecs and scopes remain
applied. Eager relation requests must be collected explicitly. Row keys use
`expr.Group(rowValue)`. Selected keys use `selected.Key()`; these types and selected
ordering cannot convert to row keys or model ordering. `HavingAnd`, `HavingOr` and
`HavingPredicate.Not` retain group scope. The compiler rejects ungrouped selected
columns, nested aggregates, aggregates in row conditions and invalid group keys.

`rows.Distinct(q)` removes duplicate complete results. `rows.DistinctOn(q, rowKeys...)`
and `rows.DistinctOnValues(q, selectedKeys...)` select one result per key combination.
Order by the DISTINCT ON keys first, then precedence and a unique tie breaker;
without ordering the chosen row is unspecified. These operations replace earlier
DISTINCT clauses, preserve result types and remain read-only. PostgreSQL grouping
and DISTINCT/order validation still apply. Existing `rows.Query.OrderBy` accepts
row orders; the package function additionally accepts selected ordering, including
aggregate results. General subquery and window entry points belong to later slices.

Relation slots reuse these descriptors through `Related(source, computation)`.
Bind the result with `Users.OrdersCount.Using(...)`, then load it through the existing
ORM relation APIs. `orm.Pivot(relation)` selects a joined relation's concrete pivot
input. See the [typed aggregate slot recipe](../README.md#typed-relation-aggregate-slots)
for ownership, nullability and statement-count contracts.
## Raw fragments and project policy

`expr.Raw[S,V](sql, args...)` produces a selected value. Built-in codecs cover
primitive scalars, exact decimals, temporal values, intervals, bytes, model IDs,
and nullable forms of the concrete built-in scalar types. Use
`expr.RawWithCodec[S](codec, sql, args...)` for custom results, named nullable
values, JSON, or another explicit representation. The caller declares the result
type; PostgreSQL checks the SQL's meaning, and the codec validates decoded values.
Raw selected values cannot become row predicates or GROUP BY keys.

`orm.WhereRaw[M](sql, args...)` produces a model predicate for `Query.Where` and
`orm.And`/`orm.Or`/`orm.Not`. Both fragment forms preserve the surrounding query's
soft-delete/global scopes, executor and transaction. Names inside SQL are opaque:
write the correct table alias yourself. Raw SQL is not automatically requalified.

Use `?` for each argument and `??` for a literal question mark (including PostgreSQL
JSON operators). The lexer leaves quoted strings/identifiers, dollar quotes and
comments untouched; it handles escaped `E'...'` strings, newline string continuations retaining escape
mode, and nested block comments. Ordinary non-E strings containing a backslash
are rejected because their interpretation depends on `standard_conforming_strings`;
use an `E'...'` string or a bound parameter.
It rejects numbered/named placeholders, count mismatches, unterminated regions,
unbalanced parentheses, NUL bytes and statement separators. Arguments use the
standard SQL driver conversion contract and are snapshotted once, including byte
buffers. Bindings join the compiler's statement-wide numbering. SQL grammar,
identifier existence, SQL types and aggregate semantics remain database checks.

Statements that render a raw fragment are opaque to the N+1 detector: diagnostics
contain a keyed fingerprint, never a trusted SQL shape or the fragment text.

Projects can enforce D11 in CI with the existing Foundry CLI:

```sh
foundry lint --no-raw ./...
```

This dependency-free tool is a **best-effort source-policy check, not a runtime
proof**. It loads selected Go packages and their tests with the current build
environment. It diagnoses calls to executor `Exec`/`Query`/`QueryRead`/`ScanOne`,
`database.ReadQuery`/`ForEach`, `Table.Raw`, `rows.Raw`, `expr.Raw`/`RawWithCodec`,
and `orm.WhereRaw`. Direct calls resolve framework symbols through generic
instantiation and promoted methods. Calls inside framework packages are excluded.

Calls through function values use their complete static signature, including
results. A variable, field, parameter or expression such as `*(&f)` is flagged
when its signature is identical to a raw entry point's method-value or function
signature, including generic instantiations. Callable type parameters are checked
using the function signature fixed by their constraint, including `~func(...)`,
embedded constraints and unions of function types with the same signature.
This rule is independent of assignment
order and does not track bindings: even an application implementation with the same
signature is flagged when called through a value. Direct named application
functions still require a framework symbol match. Captures and transfers without
calls produce no diagnostic; ordinary typed ORM usage remains allowed.

Interface and type-parameter methods named like raw executor methods are checked
against the instantiated constraint's parameter shape, regardless of result type.
This includes `Runner[R]` with `Query(context.Context, string, ...any) (R, error)`
and restated `Raw(context.Context, database.Executor, string, ...any)` methods.
A captured function value with an unresolved generic result type need not match a
framework signature; the direct interface method call still matches its parameter
contract. Diagnostics contain only `file:line`. Real package paths ending in
`.test` are checked; only Go-generated test mains are excluded.

Any finding or package loading/type-checking error fails the command. Diagnostics
omit SQL and arguments. Dependencies outside the selected patterns are loaded for
type information, not linted; select project packages deliberately. Build tags and
platforms follow `go list`, so run the command in each CI build configuration you
support. Reflection and raw calls hidden behind unselected dependency code are
outside this source policy. Function-value calls whose generic constraints leave
the result signature unresolved can remain undiagnosed; this check does not
specialize application generic bodies at every instantiation or trace wrappers.
Conversely, same-signature application function values are flagged even when
their implementations perform no SQL. No suppression comments or automatic edits
are used.
