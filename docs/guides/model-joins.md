# Typed joins and model aliases

Joins connect typed sources and can select complete models or [declared projection records](model-projections.md). Generated field sets retain each model's field operators, value types and codecs in the joined scope. The [consumer acceptance packages](../../tests/fixtures/consumer/joinqueries) and [result declarations](../../tests/fixtures/consumer/reports/join_projections.go) demonstrate inner, left, right, full and chained self-joins against PostgreSQL.

## Name each model reference

Give each occurrence a named Go tag and a SQL alias. The tag participates in the Go type; the SQL alias identifies that occurrence in the compiled statement.

```go
type referralAlias struct{}
type sponsorAlias struct{}

referrals := query.As[referralAlias](models.QueryUsers(), "referral")
sponsors := query.As[sponsorAlias](models.QueryUsers(), "sponsor")
r := models.UserFieldsAt(referrals.Scope())
s := models.UserFieldsAt(sponsors.Scope())
joined := query.LeftJoin(referrals, sponsors, query.On(r.IntroducerID, s.ID))
```

The two `User` references have different field/predicate types. `On` requires compatible key types and preserves left/right ownership, including model-owned IDs and natural keys. Nullable keys retain their non-null key type for compatibility; absent keys do not match through SQL equality. `OnAnd` supports composite comparisons, `OnOr` alternatives, and `Not` negation. Empty condition lists are invalid.

Alias names must be valid single SQL identifiers. Reusing a SQL name or the same typed alias identity in one join chain fails before execution. Different variables with the same Go tag do not create different types; use distinct named tags for self-joins. A scope from another runtime alias name is rejected even when its Go tag is the same.

`UserFields()` remains the base-model accessor. Its `UserFieldSet` type aliases `UserScopedFieldSet[User]`. `UserFieldsAt(scope)` binds those same fields and operator restrictions to a typed alias/join scope. Generated nullable accessors are described below. These are ordinary Go declarations that gopls can inspect.

## Select a complete model

Use `SelectRecord` when the result should contain all persisted fields from one preserved side. Using the `joined` left join above:

```go
scope := query.LeftScope(joined, referrals.Scope())
fields := models.UserFieldsAt(scope)
builder := query.SelectRecord(joined, scope).
    Where(fields.Email.Contains("john")).OrderBy(fields.Email.Asc())

users, err := builder.All(ctx, db)         // []models.User
user, err := builder.RequireFirst(ctx, db) // models.User or an error
```

`SelectRecord` returns the existing `ProjectionQuery[Input, Record]`: the joined input scope stays distinct from its complete result type. `Where` derives a builder without database work. `First` returns an optional record, `RequireFirst` requires a record, and `All` collects an ordinary typed Go slice. These read-only selections expose no model writes or key lookups. Order explicitly for deterministic first-row selection; join selection does not add a primary-key ordering automatically.

The framework reuses the original generated column order and decoder. A model's relation/computed slots stay unloaded; use the model query's explicit `Load` operation on returned model slices when needed. Duplicate join rows remain duplicates. Selecting a complete declared report through its aliased scope returns that report type with its original aliases and codecs. Complete selections compose with `As`, [CTEs](model-ctes.md) and [set operations](model-set-operations.md), including unions with plain queries returning the same model.

Obtain the scope from a model query, alias or set and bring it into the joined scope through `LeftScope`/`RightScope`. Nullable outer sides cannot be supplied to `SelectRecord`; use a declared nullable projection below. Zero scopes, field-only `DeclareModelScope` declarations, mismatched runtime aliases, missing metadata and eager-loading options fail before execution. The [whole-record consumer](../../tests/fixtures/consumer/recordqueries/records_postgres_test.go) verifies these boundaries, full decoding, duplicate rows and stream cleanup.

## Select an outer-join result

Bring each side into the join's resulting scope. A missing right row in a left join requires nullable fields, including a normally non-nullable ID, email or enum.

```go
referral := models.UserFieldsAt(query.LeftScope(joined, referrals.Scope()))
sponsor := models.UserNullableFieldsAt(query.NullableRightScope(joined, sponsors.Scope()))

report := reports.ProjectReferralRow(joined).
    SelectID(referral.ID.Value()).
    SelectEmail(referral.Email.Value()).
    SelectIntroducerID(sponsor.ID.Value()).
    SelectIntroducerEmail(sponsor.Email.Value()).
    SelectIntroducerNickname(sponsor.Nickname.Value()).
    SelectIntroducerStatus(sponsor.Status.Value()).
    Query().OrderBy(referral.Email.Asc())
rows, err := report.All(ctx, db)
```

The generated `ProjectReferralRow` builder infers the joined scope. Each setter checks its expression's scope and destination type. `Query()` produces the same `ProjectionQuery[Input, Result]` as the selection-struct API; compilation/execution verifies that all result fields were selected. Setters return new builder values, and selecting the same field again replaces that selection.

`UserNullableFieldsAt` accepts a `NullableModelScope`, which cannot be supplied to ordinary `UserFieldsAt`. Fields already nullable remain singly nullable. IDs retain their concrete model type inside `value.Nullable`; enum membership validation is preserved for present values. Nullable expressions cannot fill non-nullable projection fields. An inner join's field scope also cannot be used with a corresponding outer join: join kind is part of scope identity.

| Join constructor | Left model scope | New right model scope |
| --- | --- | --- |
| `InnerJoin` | `LeftScope` | `RightScope` |
| `LeftJoin` | `LeftScope` | `NullableRightScope` |
| `RightJoin` | `NullableLeftScope` | `RightScope` |
| `FullJoin` | `NullableLeftScope` | `NullableRightScope` |

Use ordinary `ModelFieldsAt` for preserved scopes and `ModelNullableFieldsAt` for nullable scopes. When a previous join already made a left-side model nullable, use `LeftNullableScope` to carry that state through another join. Accessing nullable values performs no database work.

## Source filters, ON and WHERE

Apply model filters/order/windows before `As` when they should constrain a source before joining:

```go
sponsors := query.As[sponsorAlias](
    models.QueryUsers().Where(models.UserFields().Status.Eq(models.StatusDisabled)).
        OrderBy(models.UserFields().Age.Desc()).Limit(1),
    "sponsor",
)
```

A bare input compiles as an aliased table. Filtered, ordered or windowed inputs compile as derived SELECT sources with their original metadata and bound parameters. Foundry does not move these filters into the final WHERE clause. A source window is global to that input, not a per-parent relationship limit. Apply final ordering on the projection when the returned order matters.

`On(...).WhereLeft(...)` and `.WhereRight(...)` add typed predicates to the join condition. Projection `Where` uses fields brought into the joined scope and filters the joined rows. ON restrictions and final WHERE restrictions have different effects on unmatched outer rows, following PostgreSQL's [table-expression semantics](https://www.postgresql.org/docs/18/queries-table-expressions.html#QUERIES-JOIN). Use explicit authorization/soft-delete filters until their owning framework integrations arrive.

Join formation itself exposes no model `All`, writes or hidden hydration. Eager-loading clauses and relation-loading limits on an aliased source are rejected. Projected results use their complete generated decoders; they do not masquerade as partial models.

## Chains and aggregates

Append a new aliased model to the right of an existing chain. Build `On` with fields in the previous joined scope and the new alias's scope. Lift retained scopes through the new join before selecting/filtering the final result. The consumer fixture joins referrals to their sponsors and then to sponsor orders, preserving nullable sponsor fields with `LeftNullableScope`.

Joins retain SQL row multiplicity: two matching orders produce two rows, and multiple branches can multiply rows. Projections do not deduplicate by model ID. Grouping, typed HAVING, aggregate ordering, `Each`, `First`, `Count`, `Exists` and limit/offset use the same projection runtime. `Count` counts result rows/groups in the selected window. To count matching related rows in an outer join, aggregate a nullable right-side key with `field.Count()`; it excludes unmatched NULL values.

The compiler owns source naming, binding and nested SELECT scope. Derived sources cannot accidentally reference the outer query. Expression/parameter limits span nested compilation; depth checks reject cyclic or excessively nested private ASTs. Invalid alias identities, missing mappings, out-of-scope fields and ungrouped columns fail before SQL. Canceled contexts avoid execution; callback/decoding failures close rows, and `All` discards partial collected results. Callers choose bounded collection windows or streaming for large joins.

Each right input may be an aliased model or [declared projection query](model-subqueries.md), while the left input may also be a join chain. A complete selection can wrap a joined result before using it as a right source. Generated record fields retain the same typed operators and outer nullability. Ordinary and [explicitly correlated subqueries](model-correlations.md), [nonrecursive CTEs](model-ctes.md), [recursive CTEs](model-recursive-ctes.md) and [set operations](model-set-operations.md) share this SELECT compiler. Field-to-field equality and range predicates preserve their joined/correlated scope and value types; the existing `On` constructor remains an equality key join. [Value comparisons and join conditions](value-comparisons-and-joins.md) add computed operands, non-equality ON conditions, explicit NULL-aware comparisons and `CrossJoin`. That guide covers retained input windows, outer nullability and PostgreSQL FULL JOIN planner restrictions. [Milestone 06](../../blueprint/06-relations-and-advanced-queries.md) records remaining work. [Lateral joins](lateral-joins.md) add explicitly correlated complete records with per-parent windows and generated reports.
