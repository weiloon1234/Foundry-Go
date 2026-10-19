# Typed field computations

Stored fields expose ordinary predicates, ordering, grouping and writes as methods.
Optional value/aggregate construction uses typed functions so unused computation
families are not instantiated for every owner/value combination. This is a public
API change; all handwritten consumers must migrate with the matching framework.
Generated field declarations keep their types, layout and ownership manifests.

| Previous stored-field call | Current call |
| --- | --- |
| `f.Value()` | `query.Value(f)` |
| `f.Param(v)` | `query.Param(f, v)` |
| `f.Count()` | `query.CountField(f)` |
| `f.CountDistinct()` | `query.CountDistinctField(f)` |
| `f.Min()` / `f.Max()` | `query.Min(f)` / `query.Max(f)` |
| `f.Sum()` / `f.Avg()` | `query.Sum(f)` / `query.Avg(f)` |

Bound method values and custom interfaces using a removed stored-field method
also need updating. For example, replace `sum := f.Sum` with a closure that
calls `query.Sum(f)` and returns the same concrete aggregate type. Update
templates/reflection that inspect those removed methods as well; keeping
compatibility forwarding methods would retain
the expensive method families this change removes.

`query.Count[Model]()` still counts rows. Computed row expressions and aggregates
retain their `Value` and `Param` methods. Already selected expressions, including
window results, retain `Param` and need no value promotion. Do not mechanically
replace unrelated methods such as query execution `Count(ctx, db)`,
`driver.Valuer.Value()` or a computed row expression/aggregate's `Value()`.

```go
f := models.OrderFields()
total := query.Sum(f.TotalCents)
amounts := query.SelectValue(models.QueryOrders(), total.Value())
next := query.Add(f.TotalCents, query.Param(f.TotalCents, int64(1)))
_ = amounts
_ = next
```

The sealed `RowValue`, `FieldSource`, `OrderedKeyField` and `SummaryField`
descriptors preserve concrete scope/value types. The functions infer and retain
the concrete source field type, avoiding a new interface-box allocation. A nullable field selects
`value.Nullable[T]`; its parameter still accepts the same non-null `T` as `Eq`.
Integers and exact decimals aggregate into `value.Nullable[decimal.Decimal]`,
floats into `value.Nullable[float64]` and intervals into their nullable interval
type. Text/JSON/binary values cannot become numeric summaries. Aggregates and
window results cannot become row predicates. Typed nil descriptors fail validation
before execution. SQL compilation, codec binding and transaction ownership use
the existing runtime paths.

Compilation still creates concrete typed code for capabilities actually used.
This change reduces unused method families; it does not make large generic
applications or race builds cost-free. See the [measurement guide](developer-resource-measurements.md).
