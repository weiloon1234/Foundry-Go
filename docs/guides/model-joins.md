# Typed joins and scoped fields

Use distinct ordinary Go alias tags with `rows.As[Tag](modelQuery, sqlName)`.
The alias preserves its input SELECT, including filters, ordering and windows.
`rows.Scope(alias)` and `expr.At(scope, column)` expose model columns without
constructing a second field set. For projected records use `rows.AsRecord` and
`rows.At(scope, schema.Field)`.

## Join typed inputs

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/orm/expr`, `database/orm/rows`.

`models.User` is the application's model, with a nullable `IntroducerID` naming the user who introduced it. Two empty tag types alias the same users table as referrals and sponsors:

<!-- compiled-source: tests/fixtures/consumer/joinqueries/outer/guide_outer.go#join-aliases -->
```go
type ReferralAlias struct{}
type SponsorAlias struct{}
```

The block left-joins each referral to its sponsor and lifts both sides into scopes, the sponsor's nullable:

<!-- compiled-source: tests/fixtures/consumer/joinqueries/outer/guide_outer.go#model-joins -->
```go
referrals := rows.As[ReferralAlias](models.Users.Query(), "referral")
sponsors := rows.As[SponsorAlias](models.Users.Query(), "sponsor")
r := rows.Scope(referrals)
s := rows.Scope(sponsors)
joined := rows.LeftJoin(referrals, sponsors, rows.On(expr.At(r, models.Users.IntroducerID), expr.At(s, models.Users.ID)))
referralScope := rows.LeftScope(joined, rows.Scope(referrals))
sponsorScope := rows.NullableRightScope(joined, rows.Scope(sponsors))
```

`rows.On` accepts compatible keys,
including nullable keys. `OnEqual`, `OnLess`, `OnLike` and their comparison families
accept computed row operands with distinct left/right scopes. See
[value comparisons](value-comparisons-and-joins.md).

## Outer joins and complete records

Inner/cross joins preserve both sides; left/right/full joins make the absent sides
nullable. Use `LeftScope`/`RightScope` for preserved inputs and
`NullableLeftScope`/`NullableRightScope` for missing inputs. `LeftNullableScope`
preserves an already-nullable scope through another join. `expr.AtNullable` adds
one layer to required model values; `expr.AtNullableValue` preserves an existing
nullable layer. Rows fields have corresponding `rows.AtNullable*` helpers.

## Select a complete model

Select a handwritten projection with `rows.Select`. A nullable side cannot be
hydrated as a complete non-null model. `rows.SelectRecord(joined, preservedScope)`
reuses a complete model or report decoder only for a preserved side. Complete
model reads keep retrieval behavior; DTO projections do not run model hooks.

## Conditions and boundaries

ON filtering and final WHERE filtering are different for outer joins. An ON
condition can leave a parent with missing right fields; a later WHERE may remove
that row. `WhereLeft`/`WhereRight` attach typed side predicates to ON.

Join multiplicity remains SQL multiplicity; a model primary key does not imply
unique joined output. Choose final ordering and explicit distinctness as needed;
complete-record first-row selection adds no automatic primary-key ordering.
For unmatched outer rows, SQL `COUNT(*)` still counts a row while
`COUNT(right_key)` does not.
Duplicate aliases, foreign scopes, incomplete mappings and eager-source clauses
fail before I/O. Runtime constraints such as FULL JOIN operator support remain
PostgreSQL responsibilities.

Use [derived records](model-subqueries.md), [CTEs](model-ctes.md) or
[lateral joins](lateral-joins.md) for additional SELECT boundaries. Read terminals
and pagination retain the [projection contracts](model-projections.md).
