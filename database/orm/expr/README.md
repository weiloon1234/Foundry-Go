# Scoped expressions

Choose the adapter matching the declared column family. For a text column,
use `expr.Value(orm.TextExpr(column))` for a model scope and `expr.At(rows.Scope(alias), orm.TextExpr(column))`
for an alias. `Expression[S,V,P]` distinguishes the present comparison value `V` from the
projected value `P`, including `value.Nullable[V]`.

`RowValue[S,V]` accepts stored and computed row values. `Selected[S,V]` accepts
selected results and cannot enter row predicates or row arithmetic. Promote row
values with `expr.Row(row).Value()`, or use a computed value's `Value` method. Use
`rows.MapSelected` to select an aggregate or a computed result into a declared row
field. `rows.Map` retains its existing column-expression signature.

```go
age := expr.Value(orm.NumberExpr(Users.Age))
q := expr.Where(Users.Query(), expr.Add(age, expr.Param(orm.NumberExpr(Users.Age), 2)).Gt(18))
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
values. `Param` on a nullable column takes the present value; `expr.NullableRow(v).Param(value.Null[T]())` constructs
an explicitly typed SQL NULL. Computed/selected `Param` methods take their exact
result type.

`Equal`, ordered comparisons, `Like`, `DistinctFrom` and their selected `*Value`
counterparts compare expressions with the same owner and concrete type. Selected
comparisons return `HavingPredicate`, never a row predicate. `OrderRow` and
`OrderValue` expose literal range comparisons only on ordered types. Stored enum
handles do not satisfy `GreaterColumn`'s ordered-column capability.

`expr.Count[S]()` counts rows, including NULL fields, returning required `int64` with
zero for empty input. `Exists[S]()` returns required `bool`, false for empty input,
and exposes no range comparisons. Both use the existing aggregate compiler and
preserve the source query's scopes, executor and transaction.

Text, temporal, interval, geographic and JSON functions share these row/selected
phases. See the [expression guides](../../../docs/guides/scalar-calculations.md).

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
nickname := expr.Value(orm.NullableTextExpr(Users.Nickname))
email := expr.Value(orm.TextExpr(Users.Email))
label := expr.When(nickname.IsNull(), email).
    When(email.Eq(""), expr.Param(orm.TextExpr(Users.Email), "anonymous")).
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
required `int64`, zero on empty input. `expr.Count` includes NULL rows; `Exists` returns
false on empty input. There is no general aggregate DISTINCT operation.

Each field aggregate also has an `At` constructor, such as
`expr.SumAt(rows.Scope(alias), Orders.TotalCents)`, retaining stored-field
capabilities and the alias's input scope. `.Filter(exprPredicate...)` appends
same-scope row conditions with AND for that aggregate only; it retains empty-input
semantics. Query WHERE filters every aggregate; HAVING filters completed groups.
`AggregateExpression[S,V]` is the sealed reusable descriptor for relation aggregate
slots and ad-hoc values: it retains native aggregate metadata, complete ownership and the typed
result codec/scanner. Selected calculations do not implement this interface.

```go
buyer := expr.Value(orm.ColumnExpr(Orders.BuyerID))
total := expr.Sum(orm.NumberExpr(Orders.TotalCents))
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
and DISTINCT/order validation still apply. `rows.OrderBy` accepts row and selected orders, including aggregate results.
Use the [composition APIs](../rows/README.md) for subqueries and the window
functions in this package for typed partitions, ordering and frames.

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
`database.ReadQuery`/`ForEach`, `orm.Raw`, `rows.Raw`, `expr.Raw`/`RawWithCodec`,
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


Window functions return selected values and compose with `rows.MapSelected`.
Construct a window from an existing scope; it does not copy the source query's
ordering, pagination or rows:

```go
amount := expr.Value(orm.NumberExpr(Orders.TotalCents))
w := expr.WindowFor(rows.ModelScope(Orders.Query())).
    PartitionBy(expr.Group(expr.Value(orm.ColumnExpr(Orders.BuyerID)))).
    OrderBy(amount.Asc()).
    RowsBetween(expr.UnboundedPreceding(), expr.CurrentRow())

q := rows.Select(rows.FromModel(Orders.Query()), ReportRows,
    rows.MapSelected(ReportRows.Rank, expr.RowNumber(w)),
    rows.MapSelected(ReportRows.Total, expr.Over(expr.Sum(orm.NumberExpr(Orders.TotalCents)), w)),
    rows.MapSelected(ReportRows.Previous, expr.Lag(expr.Row(amount).Value(), 1, w)),
)
```

`RowNumber`, `Rank` and `DenseRank` return `int64`; `PercentRank` and `CumeDist`
return `float64`; `NTile` returns `int32`. `Lag`, `Lead`, `FirstValue`, `LastValue`
and `NthValue` add one nullable layer because the requested row/frame may be
empty. Their nullable variants retain exactly one layer. `LagOr`/`LeadOr` retain
the input type and capture a fallback once; an existing row containing SQL NULL
still returns NULL. Offsets, bucket counts and fallbacks are bound parameters.

`expr.Over(aggregate, w)` retains the aggregate's codec and empty-frame rules:
count returns zero, sum/avg/min/max remain nullable. Apply `Filter` before `Over`.
PostgreSQL rejects COUNT DISTINCT windows. Windows cannot enter WHERE, row CASE,
GROUP BY or HAVING; nested window evaluation at the same SELECT level is invalid.
Ordinary aggregate results can supply window inputs, selected partition keys and
ordering when the surrounding grouping is valid.

`PartitionBy` takes row `GroupKey`s; `PartitionByValues` takes selected
`ProjectionKey`s. `OrderBy` accepts row and selected expression orders. Use
`expr.Value(orm.TextExpr(column)).Asc()` for a column order. Final result ordering is separate.
Named windows (`w.Named("by_buyer")`) emit each referenced definition once.
PostgreSQL's inheritance rules apply: extending a framed definition, replacing
its order or adding a partition fails compilation. Derivation is immutable.

`RowsBetween` and `GroupsBetween` accept current/unbounded positions and
`Preceding(int64)`/`Following(int64)` offsets. `RangeBetween` accepts only
current/unbounded positions. For value distances, build
`r := expr.NumericRange(w, expr.Row(amount).Value())` and then
`r.Between(r.Preceding(int64(10)), r.CurrentRow())`. Numeric distances retain the
order value's exact type. `TemporalRange` takes `temporal.Interval` distances;
nullable range constructors still require present distances. The range builder
establishes exactly one order; preexisting ordering is an error. Negative and
non-finite distances, invalid boundary ordering and calendar distances for a
clock value fail compilation. Frame exclusions support current row, group, ties
and none. A zero `Window[S]` means `OVER ()`; zero frame/range descriptors are invalid.

These package functions reuse the shared window compiler and existing ownership
checks. They add no Query, Table, column or aggregate methods. Scopes and the
supplied executor/transaction remain those of the surrounding read. Window
results enter wire contracts only through explicitly declared row/DTO fields;
this adds no manifest, OpenAPI or TypeScript schema source.

## Text, temporal, interval and geographic calculations

Scalar functions use the same SQL compiler and codecs as the shared `internal/querycore` compiler.
Pass `expr.Value(orm.TextExpr(column))` (or `expr.At(scope, orm.TextExpr(column))`) to row functions; promote
with `.Value()` for `rows.MapSelected`. The `Value` suffixed variants accept
selected values, including aggregates and windows. They never become WHERE
predicates. Use `expr.TextValue` or `expr.TextNullableValue` for selected text
conditions in `rows.Having`.

```go
name := expr.Lower(expr.Trim(expr.Value(orm.TextExpr(Users.Email))))
filtered := expr.Where(Users.Query(), name.IContains("example_"))
// Contains/IContains/StartsWith/EndsWith treat wildcards literally;
// Like/ILike accept SQL patterns. Collation belongs to the database.
```

Text functions include casing, trimming (including character sets), Unicode and
byte lengths, strict concatenation, NULL-skipping `ConcatWS`, replacement and
one-based substring positions. `SubstringAt` requires same-owner integer
expressions; literal negative counts fail before execution. Named string inputs
produce ordinary strings; nullable variants retain NULL except `ConcatWS`.

Calendar extraction accepts dates/local timestamps, clock extraction accepts
wall times/local timestamps, and epoch functions accept instants. `Second`,
`Milliseconds`, `JulianDay` and `UnixSeconds` return exact decimals;
`FromUnixMillis` requires integers. `DateUnit` and `TimestampUnit` are distinct.
Select an explicit `LoadTimeZone`/`UTCZone` for instant/calendar conversions;
`ResolveLocal` also requires `PostgresStandardTime` for DST gaps/overlaps.

`AddDateInterval` returns a local timestamp, while `AddDateDays` retains a date.
Interval calculations preserve months, days and elapsed time separately;
`ShiftDate`, `ShiftLocal` and `ShiftInstant` accept interval expressions.
`IntervalElapsed` returns `time.Duration`; clock shifts require whole
microseconds. A calendar day across DST may differ from 24 elapsed hours.
`TransactionTime(rows.ModelScope(q))` uses the surrounding transaction's start
instant and retains that source's ownership. Database overflow remains an error.

`NewGeographicPoint` validates finite latitude/longitude in degrees.
`SphericalDistance` returns approximate metres on the IUGG mean-radius sphere;
it is a surface distance, not a route. Nullable coordinates propagate NULL.
Stored coordinates remain the application's responsibility.

These functions add no Query/Table/column methods, bypass no surrounding scopes,
and use the supplied executor or transaction. Results affect manifests, OpenAPI
and TypeScript only through explicitly declared projection/DTO field codecs.
JSON paths and JSON Kind/Length remain in the next slice.

### Handwritten JSON paths

JSON paths are ordinary typed declarations beside the model columns. They do not
use generated `Properties` or `At` accessors:

```go
var Theme = expr.JSONFieldOf(func(p *Preferences) *value.Optional[string] {
    return &p.Theme
})
// During application startup, check Theme.Validate() along with orm.Check tables.

root := expr.JSONRoot(expr.Value(orm.JSONExpr(Documents.Settings)))
path := expr.JSONOptional(expr.JSONProperty(root, Theme))
theme := expr.JSONTextScalar(path, codec.String[string]())
q := expr.Where(Documents.Query(), theme.Eq("dark"))
```

`JSONFieldOf` runs its selector once against a fresh payload and resolves the
field's JSON tag with the runtime's existing tag/promotion rules. It rejects
ignored, ambiguous, foreign and zero-sized fields, and opaque custom JSON/text
encodings. Selectors address fields in that payload, including fields promoted
through embedded values; they must not dereference nil embedded pointers. Name
and `,string` metadata are captured, and the selector is discarded. A failed or
zero declaration also fails closed if used without the startup check.

`JSONMapKey(path, key)` retains the map's exact key type and uses its canonical
JSON key codec. String keys are accepted only on string-keyed maps.
`JSONIndex(path, int32Index)` retains a slice's element type; negative indices
count from the end. Fixed arrays use `JSONFixedArrayOf(func(a *[3]T) *T {
return &a[0] })` at startup, then `JSONArrayElement(path, declaration, index)`.
Byte slices and custom-encoded collections cannot be traversed as arrays/maps.
Declared property names are escaped SQL literals for expression indexes; runtime
keys and indices use the statement's normal bind parameters.

`JSONOptional`, `JSONNullable`, `JSONPointer` and `JSONDocument` explicitly unwrap
payload wrappers for traversal. `expr.JSONScalar` and `expr.JSONTextScalar` require a codec for the exact resulting payload type. They cannot
change the payload type. Compose `expr.OrderNullableRow(expr.JSONScalar(...))`
for ordered payload comparisons. All path predicates retain the original query owner.

| Operation | Missing path or SQL-null ancestor | Present JSON null |
| --- | --- | --- |
| `JSON()` | SQL NULL | Present JSON value, if its payload permits null |
| `expr.JSONTypeNullable(path.JSON())` | SQL NULL | `database.JSONNull` |
| `path.JSON().IsNotNull()` | false | true |
| `path.JSON().IsNull()` | true | false |
| `expr.JSONTypeNullable(path.JSON()).Eq(value.Of(database.JSONNull))` | does not match | true |
| scalar extraction | SQL NULL | SQL NULL |
| `expr.JSONLengthNullable(path.JSON())` | SQL NULL | SQL NULL |

`Length` is also SQL NULL for every non-array kind. `Contains` accepts a typed
payload fragment and does not match a missing path. Whole-document calculations
use `JSONType`, `JSONLength`, `JSONContains` and `JSONContainedBy`; `Nullable`
variants propagate SQL NULL and `Value` variants preserve selected phase.
Use `rows.MapSelected` for their results. Existing columns, queries and tables
acquire no new methods. JSON calculations do not create implicit public DTO,
manifest, OpenAPI or TypeScript schemas; those remain explicitly declared.

UUID seeks use `UUIDLess`, `UUIDLessOrEqual`, `UUIDGreater` and
`UUIDGreaterOrEqual` on row expressions, for example
`expr.UUIDGreater(expr.At(scope, orm.KeyExpr(Files.Key())), after)`. The UUID's model owner
and expression scope remain distinct types. Nullable row expressions use the
`Nullable` variants and compare a present UUID. Ordering is PostgreSQL bytewise
UUID ordering, not a guarantee about UUID generation time.

## Projection filter adapters

`ModelPredicate(orm.EnumEq(Users.State, Active))` retains model declaration ownership in a
projection's row phase. `Has(UserRelations.Orders, childPredicate)` lifts expression
predicates through the existing scoped relation EXISTS compiler. A child from
another declaration fails validation even when its Go model type matches. The
bound relation plan must also match both model types; a promoted target marker
from another embedded relation cannot change them.

`CompareRow(Value(Users.State))` supplies equality and membership
without adding ordered or text operations to enums. `CompareNullableRow` retains
present-value comparisons and adds null tests for nullable fields. Use `OrderRow` or
`OrderNullableRow` for computed ordered values. Stored text adapters take the
column itself: `TextRow(Users.Name)` and `TextNullableRow(Users.Nickname)`. Their
`orm.TextExpressionColumn` input rejects direct non-text handles; `Value`
erases that static capability and cannot be passed to these adapters. Both
adapters validate the retrieved column's own text metadata before granting
operations. Nested embedding cannot borrow a text marker from another handle:
an invalid adapter retains its error through comparisons, ordering, selection
and parameters. String-backed enums cannot acquire text or ordered comparisons
through these adapters. Computed text functions already return text
comparison handles. Present values remain the inputs for nullable comparisons.
The compiler also rejects literal and expression LIKE predicates without a text
capability, independently of the codec's string representation.

`Or` and `Not` accept only row predicates; `HavingOr` and `HavingPredicate.Not`
retain the group phase. Report filters must reject mixed WHERE/HAVING OR/NOT.
SQL NOT alone excludes unknown results: null-inclusive report negation uses
`expr.Or(expr.Not(note.Eq("Ada")), note.IsNull())`. Literal `Contains`/`IContains`
escape wildcards, while `Like` deliberately accepts patterns. The datatable
request policy continues to require explicit `AllowLike` and owns mixed-phase
validation and null-inclusive negation; its API cutover is a separate slice.

For joined or aliased report columns, use `TextRowAt(scope, column)` or
`TextNullableRowAt(scope, column)`. They retain the column’s actual text capability
and its validated scope; promoted markers cannot authorize an enum as text.

### Expression filters on relations

Use `expr.RelationFilter` to adapt a row predicate to an existing relation's
`.Where` method. The relation validates the target model and declaration owner;
selected/HAVING predicates and predicates for a different target cannot be passed.

```go
orders := UserRelations.Orders
orders = orders.Where(expr.RelationFilter(orders,
    expr.GreaterColumn(orm.NumberExpr(Orders.Total), orm.NumberExpr(Orders.Paid))))
users, err := orm.All(ctx, db, Users.Query().Where(orm.Has(orders)).With(orders))
```

The adapter retains correlated references and uses the same owner validation as
`expr.Has`. It also permits JSON operators on a natural primary key through
`expr.Value(orm.KeyExpr(Policies.Key()))`, without binding the primary field a second time.

`RelatedValue(source, computation)` returns a row expression with the aggregate's
exact nullability. Use existing `OrderBy`/`Where` adapters to rank and filter models,
and `rows.WithValue` to read a complete model alongside the value. See the
[complete model and computed value recipe](../rows/records.md).

Computed relation ordering uses `relation.OrderBy(expr.RelationOrder(relation,
expr.Negate(expr.Value(orm.NumberExpr(Orders.Total))).Asc()))`. For pivot fields, use
`relation.OrderByPivot(expr.RelationPivotOrder(relation, order))`. Both adapters
validate the relation's actual source/target metadata and the field declaration;
pivot ordering additionally validates the bound pivot. These row-phase adapters
preserve NULL placement and binding order and reject selected/window orders.
