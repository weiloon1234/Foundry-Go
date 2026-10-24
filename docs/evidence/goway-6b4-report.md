# Lane A — 6b4 transaction composition

Implemented on the accepted 6c3 worktree without Git writes. The prior slice
artifacts and lead-owned docs are unchanged. Stage 6b now has 100/100 catalogue
rows ported; the final transaction_nullable_record case brings 6c to 73/73.

## API and shared owners

- Transaction inner/left/right/full/cross joins consume transaction aliases and
  chains. Existing scope selectors preserve their transaction identity and widen
  missing sides; alias metadata and clauses are checked together by JoinSources.
- Transaction scalar, nullable scalar, row-scalar, IN and EXISTS functions retain
  exact value codecs and the transaction anchor. TransactionScalarOf(selected)
  supplies typed lock composition, including targets and independent lock clauses.
  Existing rows.ForUpdate(selected) remains the executable locked-result path.
- TransactionCorrelate and explicit outer/inner scope lifting support nested
  enclosing scopes. Correlated scalar/record results remain non-executable and
  cannot become independent or ordinary sources. Projection mappings reuse the
  ordinary shared helper. Correlated clauses include grouping/HAVING; records
  also retain DISTINCT variants.
- AsTransactionLateral binds complete correlated model/projection results to their
  preceding parent. Cross/inner/left joins are supported; ON scopes and nullable
  record restrictions match the old API. Nested locks retain their SELECT boundary.
- The existing record lock owner now exposes source-level operations to both
  executable records and non-executable correlations. No new SQL compiler,
  dependencies, Query/Table methods, embedding chains or public capability bridges.
  Existing validated sqlowner families are reused.

Entry functions added:

- `rows.AsTransactionLateral`
- `rows.ProjectTransactionCorrelated`
- `rows.SelectTransactionCorrelatedRecord`
- `rows.SelectTransactionCorrelatedScalar`
- `rows.TransactionCorrelate`
- `rows.TransactionCorrelatedScalarNullableQuery`
- `rows.TransactionCorrelatedScalarNullableRowQuery`
- `rows.TransactionCorrelatedScalarQuery`
- `rows.TransactionCorrelatedScalarRowQuery`
- `rows.TransactionCrossJoin`
- `rows.TransactionCrossJoinLateral`
- `rows.TransactionExistsQuery`
- `rows.TransactionFullJoin`
- `rows.TransactionInCorrelatedQuery`
- `rows.TransactionInNullableCorrelatedQuery`
- `rows.TransactionInNullableQuery`
- `rows.TransactionInQuery`
- `rows.TransactionInnerJoin`
- `rows.TransactionInnerJoinLateral`
- `rows.TransactionInnerNullableScope`
- `rows.TransactionInnerScope`
- `rows.TransactionLateralScope`
- `rows.TransactionLeftJoin`
- `rows.TransactionLeftJoinLateral`
- `rows.TransactionOuterNullableScope`
- `rows.TransactionOuterScope`
- `rows.TransactionRightJoin`
- `rows.TransactionScalarNullableQuery`
- `rows.TransactionScalarNullableRowQuery`
- `rows.TransactionScalarOf`
- `rows.TransactionScalarQuery`
- `rows.TransactionScalarRowQuery`
- `rows.TransactionSubqueryOf`
- `rows.TransactionValueOf`

## Catalogue, regressions and PostgreSQL

36 existing catalogue rows ported with individual mapping notes. The compiler
harness now has 477 cases. The new runtime tests exercise nested locks, caller
transaction execution, nullability, zero descriptors, wrong declarations, borrowed
and promoted join markers, nested correlations, projected LATERAL and missing-lock
policies. Sixteen old/new SQL and binding goldens cover all five join kinds,
scalar/nullable/IN/EXISTS subqueries, correlation and nesting, and four LATERAL
forms. PostgreSQL sources cover typed results, NULL widening, nested locks and
unchanged persisted rows after commit; compiled and vetted, not executed here.

Rows ported:

`lock_clause_nullable`, `locked_nullable_target`, `transaction_correlated_parent`, `transaction_correlated_projection_type`, `transaction_correlated_record_execute`, `transaction_correlated_record_ordinary`, `transaction_correlated_source_conversion`, `transaction_correlated_value_execute`, `transaction_correlated_value_ordinary`, `transaction_correlated_value_unbound`, `transaction_correlation_inner_scope`, `transaction_correlation_ordinary_source`, `transaction_correlation_outer_scope`, `transaction_correlation_scope_conversion`, `transaction_correlation_unbound_record`, `transaction_correlation_unlifted_scope`, `transaction_exists_source`, `transaction_lateral_conversion`, `transaction_lateral_nullable_record`, `transaction_lateral_on_scope`, `transaction_lateral_ordinary_join`, `transaction_lateral_parent`, `transaction_lateral_pool`, `transaction_lateral_unbound`, `transaction_nullable_lock`, `transaction_nullable_record`, `transaction_scope_conversion`, `transaction_source_alias`, `transaction_source_join`, `transaction_value_nullability`, `transaction_value_operand_type`, `transaction_value_ordinary_anchor`, `transaction_value_owner`, `transaction_value_scalar_escape`, `transaction_value_selected_operand`, `transaction_value_source`.

## Footprint

Existing ceilings are unchanged. New ceilings use ceil(measured × 1.15).
Measurements use native warm-cache archive T/t-symbol counts, not runtime timing.
The nearly full common probes have zero growth; new methods remain on opt-in
transaction families, while shared lock work is non-generic.

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| transaction_joins | 1835 | 2111 |
| transaction_correlation | 2908 | 3345 |
| q_with | 1209 | 1211 |
| aggregate_families | 4366 | 4369 |

## Surface additions

The public snapshot has 181 added lines and no removals. Each addition is listed
below exactly as captured; repeated method lines represent distinct receiver types.
No existing exported signature changed.

```text
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
  Exists func() github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
rows.TransactionCorrelatedRecord
  Distinct func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Distinct func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  DistinctOn func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  DistinctOn func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  DistinctOnValues func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  DistinctOnValues func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  GroupBy func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  GroupBy func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Having func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.HavingPredicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Having func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.HavingPredicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionOrder[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionOrder[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
*rows.TransactionCorrelatedRecord
rows.TransactionCorrelatedScalar
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  GroupBy func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  GroupBy func(keys ...github.com/weiloon1234/Foundry-Go/database/orm/expr.GroupKey[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Having func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.HavingPredicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Having func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.HavingPredicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionOrder[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.ProjectionOrder[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
*rows.TransactionCorrelatedScalar
rows.TransactionCorrelatedSource
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  Limit func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  Offset func(n int) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Order[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  OrderBy func(os ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Order[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
  Where func(ps ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
*rows.TransactionCorrelatedSource
rows.TransactionCorrelation
*rows.TransactionCorrelation
rows.TransactionCross
*rows.TransactionCross
rows.TransactionCrossJoined
*rows.TransactionCrossJoined
rows.TransactionFull
*rows.TransactionFull
rows.TransactionFullJoined
*rows.TransactionFullJoined
rows.TransactionInner
*rows.TransactionInner
rows.TransactionInnerJoined
*rows.TransactionInnerJoined
rows.TransactionJoinSource
  transactionSelection func(*S) github.com/weiloon1234/Foundry-Go/internal/ormcore.RecordSource
  transactionSelection func(*S) github.com/weiloon1234/Foundry-Go/internal/ormcore.RecordSource
rows.TransactionJoinTable
  transactionJoinTable func(*S)
rows.TransactionLateralSource
*rows.TransactionLateralSource
rows.TransactionLeft
*rows.TransactionLeft
rows.TransactionLeftJoined
*rows.TransactionLeftJoined
rows.TransactionRight
*rows.TransactionRight
rows.TransactionRightJoined
*rows.TransactionRightJoined
rows.TransactionScalar
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForKeyShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForNoKeyUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForShare func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  ForUpdate func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  LockRows func(cs ...github.com/weiloon1234/Foundry-Go/database/orm/rows.RowLock[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  NoWait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  Of func(ts ...github.com/weiloon1234/Foundry-Go/database/orm/rows.LockTarget[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  SkipLocked func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
  Wait func() github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
*rows.TransactionScalar
rows.TransactionScopeSource
  transactionCorrelationScope func(*S) github.com/weiloon1234/Foundry-Go/internal/ormcore.VisibleScope
rows.TransactionScopeType
  transactionScopeType func()
rows.TransactionSubquery
*rows.TransactionSubquery
rows.TransactionSubquerySource
  transactionSubquery func() github.com/weiloon1234/Foundry-Go/internal/ormcore.Source
rows.TransactionValueInput
*rows.TransactionValueInput
rows.TransactionValueSource
  transactionValue func(*V) (github.com/weiloon1234/Foundry-Go/internal/ormcore.RecordSource, github.com/weiloon1234/Foundry-Go/internal/ormcore.Column[V])
rows.ProjectTransactionCorrelated func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, R any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], schema github.com/weiloon1234/Foundry-Go/database/orm/rows.Schema[R], mappings ...github.com/weiloon1234/Foundry-Go/database/orm/rows.Mapping[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
rows.SelectTransactionCorrelatedRecord func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, R any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], scope github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R]
rows.SelectTransactionCorrelatedScalar func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], v github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], V]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]
rows.AsTransactionLateral func[A any, O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, R any](record github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedRecord[O, I, R], alias string) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLateralSource[A, O, R]
rows.TransactionCorrelate func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeSource[O], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[I]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I]
rows.TransactionCorrelatedScalarNullableQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCorrelatedScalarNullableRowQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.RowExpression[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCorrelatedScalarQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCorrelatedScalarRowQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.RowExpression[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCrossJoin func[L, R github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinTable[R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCrossJoined[L, R]
rows.TransactionCrossJoinLateral func[L github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, A, R any](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLateralSource[A, L, R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCrossJoined[L, github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R]]
rows.TransactionExistsQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSubquerySource) github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[S]
rows.TransactionFullJoin func[L, R github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinTable[R], on github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionFullJoined[L, R]
rows.TransactionInCorrelatedQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](v github.com/weiloon1234/Foundry-Go/database/orm/expr.RowValue[O, V], s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
rows.TransactionInNullableCorrelatedQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](v github.com/weiloon1234/Foundry-Go/database/orm/expr.RowValue[O, V], s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[O]
rows.TransactionInNullableQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](left github.com/weiloon1234/Foundry-Go/database/orm/expr.RowValue[S, V], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[S]
rows.TransactionInQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](left github.com/weiloon1234/Foundry-Go/database/orm/expr.RowValue[S, V], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Predicate[S]
rows.TransactionInnerJoin func[L, R github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinTable[R], on github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionInnerJoined[L, R]
rows.TransactionInnerJoinLateral func[L github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, A, R any](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLateralSource[A, L, R], on ...github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionInnerJoined[L, github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R]]
rows.TransactionInnerNullableScope func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, M any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], scope github.com/weiloon1234/Foundry-Go/database/orm/expr.NullableScope[I, M]) github.com/weiloon1234/Foundry-Go/database/orm/expr.NullableScope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], M]
rows.TransactionInnerScope func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, M any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], scope github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[I, M]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], M]
rows.TransactionLateralScope func[A any, O github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, R any](source github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLateralSource[A, O, R]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R], R]
rows.TransactionLeftJoin func[L, R github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinTable[R], on github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLeftJoined[L, R]
rows.TransactionLeftJoinLateral func[L github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, A, R any](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLateralSource[A, L, R], on ...github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R]]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionLeftJoined[L, github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionAlias[A, R]]
rows.TransactionOuterNullableScope func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, M any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], scope github.com/weiloon1234/Foundry-Go/database/orm/expr.NullableScope[O, M]) github.com/weiloon1234/Foundry-Go/database/orm/expr.NullableScope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], M]
rows.TransactionOuterScope func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, M any](c github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedSource[O, I], scope github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[O, M]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Scope[github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelation[O, I], M]
rows.TransactionRightJoin func[L, R github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType](left github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinSource[L], right github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionJoinTable[R], on github.com/weiloon1234/Foundry-Go/database/orm/rows.JoinOn[L, R]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionRightJoined[L, R]
rows.TransactionScalarNullableQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionScalarNullableRowQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.RowExpression[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionScalarOf func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](q github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValue[S, V]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScalar[S, V]
rows.TransactionScalarQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionScalarRowQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.RowExpression[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionSubqueryOf func[S any](source github.com/weiloon1234/Foundry-Go/database/orm/rows.SelectionSource[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSubquery
rows.TransactionValueOf func[V any](source github.com/weiloon1234/Foundry-Go/database/orm/rows.ValueSource[V]) github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueInput[V]
```

## Shared files

Existing files changed relative to accepted 6c3 (all other changes are new slice files):

- `blueprint/goway-data-layer/compilefail-mapping.tsv`
- `database/orm/method_surface_test.go`
- `database/orm/rows/README.md`
- `database/orm/rows/correlated_record.go`
- `database/orm/rows/result_read.go`
- `database/orm/testdata/public-methods.txt`
- `internal/ormcore/record_lock.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/footprint_test.go`

## Acceptance

All Go commands used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Command | Result |
| --- | --- |
| go build ./... | PASS |
| go vet ./database/... ./internal/... | PASS |
| go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... | PASS |
| fixture: go vet ./... | PASS |
| fixture: go test ./... | PASS |
| python3 tools/goway_catalogue.py | PASS |
| repository gofmt check | PASS |
| git diff --check | PASS |
| make docs-check | PASS |

Initial verification found test-source mistakes (a key range method, constructor
spelling, a fixture import collision, scoped-expression range calls, and an
ambiguous adversarial wrapper), plus mismatched old/new parameter constructors in four goldens, the expected additive snapshot and an EXISTS
spacing assertion. Corrected before the final passing gate; raw logs retained.
PostgreSQL URL and required controls were cleared for fixture tests. No PostgreSQL
execution, commit or other Git writes.

## Docs notes

Changelog/blueprint note for the lead: complete Stage 6b transaction-required
composition using the Stage 6c record adapters. Add transaction joins, scoped
subqueries, nested correlation and cross/inner/left LATERAL with preserved locks,
nullability and executor restrictions. Link the package transaction recipe.
Stage 6b catalogue: 100/100; Stage 6c catalogue: 73/73. Existing footprint ceilings
unchanged; two new probes measured with 15% headroom.

## Questions for the lead

None. Run the new pgsmoke source at the integration checkpoint. The 6b4-only patch
is /private/tmp/goway-6b4.patch, based on the accepted 6c3 worktree snapshot.

## Review correction: transaction scope embedding (last-11)

Fixed the Medium finding in both ORM rows and the old query API. Transaction
adapters stamp `querycore.TableSource.RequiresTransaction` when lowering their
metadata. The shared SELECT compiler rejects these sources outside transaction
compilation, including unlocked values. Direct aliases copy this metadata;
derived aliases retain the original SELECT; joins retain each input source.
CTEs, sets and cursors retain their nested SELECTs. No marker method grants runtime
permission, and no separate SQL compiler or application capability was added.

Regressions cover ordinary models embedding the interface and concrete transaction
markers; nullable and non-nullable membership; selected and lifted values; all five
join kinds; All/Count/Exists rejection and no executor calls; and valid transaction
composition. Shared compiler regressions isolate left/right requirements for each
join kind with direct and derived aliases, plus IN/EXISTS/scalar, CTE and both set
operands. Existing lock, correlation, LATERAL and SQL parity coverage also passed.

API/surface additions for this correction: none. Catalogue rows unchanged (6b
100/100, 6c 73/73); runtime regressions cover the embedding loophole. Existing
footprint ceilings are unchanged; no new probes or dependencies.

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| transaction_joins | 1835 | 2111 |
| transaction_correlation | 2908 | 3345 |
| q_with | 1209 | 1211 |
| aggregate_families | 4366 | 4369 |

Correction files (shared source files and regression/doc additions):

- `database/orm/rows/transaction_record.go`
- `database/orm/rows/transaction_requirement_test.go`
- `database/orm/rows/transaction_subquery.go`
- `database/orm/rows/transactions.md`
- `database/query/transaction_alias.go`
- `database/query/transaction_cte.go`
- `database/query/transaction_requirement_test.go`
- `database/query/transaction_source.go`
- `database/query/transaction_subquery.go`
- `database/query/transaction_value.go`
- `internal/querycore/select.go`
- `internal/querycore/transaction_requirement_test.go`

Acceptance rerun with GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2,
GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off:

| Command | Result |
| --- | --- |
| go build ./... | PASS |
| go vet ./database/... ./internal/... | PASS |
| go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... | PASS |
| fixture: go vet ./... | PASS |
| fixture: go test ./... | PASS |
| python3 tools/goway_catalogue.py | PASS |
| repository gofmt check | PASS |
| git diff --check | PASS |
| make docs-check | PASS |

Logs: `/private/tmp/goway-6b4-review-logs`. PostgreSQL sources compiled and vetted;
execution controls blank. No PostgreSQL execution or Git writes. Public surface,
catalogue mapping, all ceilings and accepted 6c evidence were preserved.

### Docs notes

Lead changelog note: transaction-required sources retain their executor requirement
through aliases, joins and subqueries even without locks. Embedding a transaction
scope marker cannot permit ordinary pool execution. The same shared compiler guard
protects the legacy query API. No lead-owned documents edited.

No questions for the lead. Full 6b4 patch: `/private/tmp/goway-6b4.patch`.
Correction-only patch: `/private/tmp/goway-6b4-review-fix.patch`.

## Review correction: explicit transaction conversions (last-12)

Legacy `TransactionRecord` now has its own private phase field and stamps the
stored SQL source in `TransactionOf`. ORM `rows.TransactionOf` also stamps its
stored source immediately. The shared compiler guard remains the execution owner.
The sweep found one additional identical public layout: ORM `LockedQuery` versus
`Query`. A distinct private locked phase now prevents that conversion as well.

The persistent layout sweep checks all 52 public transaction/locked structs in
ORM, rows and legacy query against every exported struct in their package.
Generic parameter constraints are conservatively erased to compare layouts;
private phase fields and nested named scope types retain their identities.
A test injects the original vulnerable legacy record layout and confirms the sweep
detects its conversion to `SetQuery`. No other identical public layouts were found.

Runtime regressions bypass transaction getters internally and prove the stored
record rejects ordinary set execution and composition through aliases, CTEs and
cursors; the original ordinary set remains usable and transaction composition works.
Two independent compile-fail cases reject the legacy record-to-set conversion
and ORM locked-to-ordinary conversion. Compiler cases: 479. Catalogue rows unchanged
(6b 100/100; 6c 73/73); these two cases are new safety regressions, not catalogue ports.

Public surface additions: none. No Query/Table methods, dependencies, new probes
or raised ceilings. Measurements after the correction:

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| transaction_joins | 1835 | 2111 |
| transaction_correlation | 2908 | 3345 |
| q_with | 1209 | 1211 |
| aggregate_families | 4366 | 4369 |

Transaction-only type inventory:

- `database/orm` (1): `LockedQuery`.
- `database/orm/rows` (25): `LockedResult`, `TransactionAlias`, `TransactionAliased`, `TransactionCommonTable`, `TransactionCorrelatedRecord`, `TransactionCorrelatedScalar`, `TransactionCorrelatedSource`, `TransactionCorrelation`, `TransactionCross`, `TransactionCrossJoined`, `TransactionFull`, `TransactionFullJoined`, `TransactionInner`, `TransactionInnerJoined`, `TransactionLateralSource`, `TransactionLeft`, `TransactionLeftJoined`, `TransactionRead`, `TransactionRecord`, `TransactionRight`, `TransactionRightJoined`, `TransactionScalar`, `TransactionSubquery`, `TransactionValue`, `TransactionValueInput`.
- `database/query` (26): `LockedQuery`, `LockedResult`, `LockedTransactionValue`, `TransactionAlias`, `TransactionAliasedSource`, `TransactionCommonTable`, `TransactionCorrelatedRecordQuery`, `TransactionCorrelatedSource`, `TransactionCorrelatedValueQuery`, `TransactionCorrelation`, `TransactionCross`, `TransactionCrossJoined`, `TransactionFull`, `TransactionFullJoined`, `TransactionInner`, `TransactionInnerJoined`, `TransactionLateralSource`, `TransactionLeft`, `TransactionLeftJoined`, `TransactionQuery`, `TransactionRecord`, `TransactionRight`, `TransactionRightJoined`, `TransactionSubquery`, `TransactionValue`, `TransactionValueQuery`.

Files changed by this correction (including shared files):

- `database/orm/query.go`
- `database/orm/rows/transaction_record.go`
- `database/orm/rows/transaction_requirement_test.go`
- `database/orm/rows/transactions.md`
- `database/orm/transaction_phase_test.go`
- `database/query/transaction_requirement_test.go`
- `database/query/transaction_source.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_transaction_record_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/locked_query_conversion/invalid.go`

Acceptance used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Command | Result |
| --- | --- |
| go build ./... | PASS |
| go vet ./database/... ./internal/... | PASS |
| go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... | PASS |
| fixture: go vet ./... | PASS |
| fixture: go test ./... | PASS |
| python3 tools/goway_catalogue.py | PASS |
| repository gofmt check | PASS |
| git diff --check | PASS |
| make docs-check | PASS |

Logs: `/private/tmp/goway-6b4-conversion-logs`. PostgreSQL sources compiled and
vetted with execution controls blank. No PostgreSQL execution or Git writes.
Public surface, all ceilings, catalogue mappings and accepted 6c evidence preserved.

### Docs notes

Lead changelog note: block explicit conversion of legacy transaction records into
ordinary set queries and ORM locked queries into ordinary model queries. Stamp
transaction records at construction so their stored source retains the execution
requirement before any getter runs. No lead-owned documents edited.

No questions for the lead. Full 6b4 patch: `/private/tmp/goway-6b4.patch`.
This correction only: `/private/tmp/goway-6b4-conversion-fix.patch`.

## Review correction: general phase conversion guarantee (last-13)

Both findings are resolved. Private zero-size fields block the four reported
legacy conversions: HavingPredicate/Predicate, ValueComparison/RowExpression,
ValueCase/Case and ProjectionKey/Group. The broader sweep also led to distinct
phases for ORM singular/collection/computed slots and relation descriptors,
legacy nullable fields versus non-nullable fields, and update versus delete plans.
Constructors retain existing metadata and behavior. There are no public methods
or compiler/executor changes in this correction.

The persistent go/types guard enumerates exported types in database/orm,
orm/expr, orm/rows and database/query without a name-prefix filter. It compares
both conversion directions, including pairs across packages, using one shared
export importer. Generic arguments are conservatively aligned to int without
constraint validation; this also reports layouts whose value constraints are
currently disjoint. Aliases of an identical type and constraint-only interfaces
are not distinct convertible value types.

The explicit commented allowlist contains **60 pairs**: 20 input-column pairs,
11 expression pairs, 2 rows pairs, 24 legacy pairs and 3 cross-package pairs.
These preserve the same input, row/selected, ordinary lookup, mapping or hook
phase; inert scope tags and scalar vocabulary also grant no execution phase.
The comments record retained ownership/codecs and validation. No family or prefix
is exempted. New unlisted pairs and stale allowlist entries fail the guard.

Sixteen independent compile-fail regressions cover every newly blocked pair.
All sixteen compile against the pre-fix source through a Go overlay, proving
that the cases exercise the original conversions. They now fail for the intended
cannot-convert diagnostic. Guard regressions restore all four reported legacy
layouts and the original transaction-record layout; a synthetic cross-package
scalar pair also proves the guard is not restricted to structs or one package.
An unnamed-alias regression checks alias conversions without misclassifying two
names for the identical type.
There are now 495 compiler cases. Catalogue mappings remain unchanged: 6b 100/100
and 6c 73/73. These are additional safety cases, not invented catalogue ports.

Public surface additions: **none**. No Query/Table methods, capabilities,
dependencies or probes were added. All **69 footprint counts are exactly equal**
to the immediately preceding source snapshot, and every existing ceiling is
unchanged. Selected measurements:

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| q_with | 1209 | 1211 |
| aggregate_families | 4366 | 4369 |
| transaction_joins | 1835 | 2111 |
| transaction_correlation | 2908 | 3345 |

Files changed by this correction (including every shared file):

- `database/orm/relation.go`
- `database/orm/relation_aggregate.go`
- `database/orm/rows/transactions.md`
- `database/orm/surface_test.go`
- `database/orm/transaction_phase_test.go`
- `database/query/conditional_expression.go`
- `database/query/expression_comparison.go`
- `database/query/expression_key.go`
- `database/query/field.go`
- `database/query/having.go`
- `database/query/source_write.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_having_predicate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_nullable_json_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_nullable_ordered_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_projection_key_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_source_mutation_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_value_case_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_value_comparison_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_aggregate_relation_belongs_to_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_aggregate_relation_has_many_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_aggregate_relation_has_one_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_belongs_to_has_many_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_belongs_to_has_one_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_has_one_has_many_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_one_many_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_value_many_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/relation_value_one_conversion/invalid.go`

The evidence JSON and this report are updated as well. Lane C expression code,
shared SQL compilers, catalogue rows, public surface snapshot, footprint budget
file, accepted 6c evidence and lead-owned documentation are unchanged.

Acceptance used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Command | Result |
| --- | --- |
| go build ./... | PASS |
| go vet ./database/... ./internal/... | PASS |
| go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... | PASS |
| final checker audit: go vet ./database/... ./internal/... and go test -race ./database/orm | PASS |
| fixture: go vet ./... | PASS |
| fixture: go test ./... | PASS |
| python3 tools/goway_catalogue.py | PASS |
| repository gofmt check | PASS |
| git diff --check | PASS |
| make docs-check | PASS |
| all 69 footprint probes: before/after T/t counts | PASS: identical |
| all 16 regression sources against pre-fix overlay | PASS: original conversions compile |

Logs: `/private/tmp/goway-6b4-phase-logs`. PostgreSQL sources compiled and vetted;
execution controls were blank and no PostgreSQL was run. No Git writes or commits.
Initial inventory runs intentionally reported unclassified conversions; the final
race run passes the completed guard. An intermediate guard build also caught an
unused test import, corrected before acceptance. The final alias-coverage
improvement affected only the guard tests: the full vet command and ORM race
tests passed again afterward; unchanged runtime/fixture results were retained.

### Docs notes

Lead changelog note: prevent explicit conversions across legacy row/selected
expression phases, relation cardinalities and computed slots, nullability and
source mutation kinds. A persistent go/types guard now covers exported types in
both APIs, including cross-package pairs, with an explicit reviewed allowlist.
The zero-size phase fields leave all measured footprint counts unchanged.
No lead-owned documents were edited.

No questions for the lead. Full 6b4 patch: `/private/tmp/goway-6b4.patch`.
This correction only: `/private/tmp/goway-6b4-phase-fix.patch`.

## Rebased allowlist audit correction (last-14)

Base: `05b830c36d594558701e13449d1928660aae7214`. This section supersedes the earlier allowlist assessment.
Both surface conflicts were resolved by union. The existing serializer regenerated
public-methods.txt; comparison against both inputs confirms no lost entries.
Relative to HEAD the snapshot has 181 additions and zero removals, all from 6b4.
This correction adds no public method, Query/Table method or runtime capability.

Findings 2–6 are resolved:

- Aggregate/OrderedAggregate have an aggregate phase distinct from Selected,
  ValueComparison and OrderedValueComparison (six removed exceptions).
- JSONField/OrderedField and their nullable variants have distinct phases (two).
- ValueLookup/UniqueLookup have distinct layouts (one).
- Legacy InsertMapping/UpdateMapping now have distinct phases (one). The rebased
  6e3 code already protected ORM mappings; inspection of HEAD confirmed the legacy
  mapping pair still needed this fix.
- The go/types sweep matches generic layout equations, including nested named and
  concrete types, different arities, slices/maps and signatures. It instantiates
  the matched arguments and verifies both conversions with go/types. Constraints
  remain conservatively erased, so impossible-constraint shapes are explicitly
  classified. Recursive substitution equations are rejected. This replaces the
  int-only sampling gap without claiming general constraint satisfiability.

Allowlist: **60 before → 111 after** = 60 − 10 removed + 61 newly inventoried.
The new entries comprise 20 pending input pairs, 21 expression-adapter pairs,
16 scalar-vocabulary pairs, three legacy adapters and one result DTO pair.
Each of the 71 non-pending entries carries its own one-line no-grant reason.
The 40 input-carrier entries are explicitly `pending: lane B roles slice`;
these remain known capability gaps under the lead's instruction. No Input* carrier
was changed. The pending pairs are:

- `orm.InputColumn <-> orm.InputEnum`
- `orm.InputColumn <-> orm.InputNumber`
- `orm.InputColumn <-> orm.InputText`
- `orm.InputColumn <-> orm.InputTime`
- `orm.InputEnum <-> orm.InputNumber`
- `orm.InputEnum <-> orm.InputText`
- `orm.InputEnum <-> orm.InputTime`
- `orm.InputNumber <-> orm.InputText`
- `orm.InputNumber <-> orm.InputTime`
- `orm.InputText <-> orm.InputTime`
- `orm.NullableInputColumn <-> orm.NullableInputEnum`
- `orm.NullableInputColumn <-> orm.NullableInputNumber`
- `orm.NullableInputColumn <-> orm.NullableInputText`
- `orm.NullableInputColumn <-> orm.NullableInputTime`
- `orm.NullableInputEnum <-> orm.NullableInputNumber`
- `orm.NullableInputEnum <-> orm.NullableInputText`
- `orm.NullableInputEnum <-> orm.NullableInputTime`
- `orm.NullableInputNumber <-> orm.NullableInputText`
- `orm.NullableInputNumber <-> orm.NullableInputTime`
- `orm.NullableInputText <-> orm.NullableInputTime`
- `orm.InputColumn <-> orm.InputInterval`
- `orm.InputColumn <-> orm.InputJSON`
- `orm.InputEnum <-> orm.InputInterval`
- `orm.InputEnum <-> orm.InputJSON`
- `orm.InputInterval <-> orm.InputNumber`
- `orm.InputInterval <-> orm.InputText`
- `orm.InputInterval <-> orm.InputTime`
- `orm.InputJSON <-> orm.InputNumber`
- `orm.InputJSON <-> orm.InputText`
- `orm.InputJSON <-> orm.InputTime`
- `orm.NullableInputColumn <-> orm.NullableInputInterval`
- `orm.NullableInputColumn <-> orm.NullableInputJSON`
- `orm.NullableInputEnum <-> orm.NullableInputInterval`
- `orm.NullableInputEnum <-> orm.NullableInputJSON`
- `orm.NullableInputInterval <-> orm.NullableInputNumber`
- `orm.NullableInputInterval <-> orm.NullableInputText`
- `orm.NullableInputInterval <-> orm.NullableInputTime`
- `orm.NullableInputJSON <-> orm.NullableInputNumber`
- `orm.NullableInputJSON <-> orm.NullableInputText`
- `orm.NullableInputJSON <-> orm.NullableInputTime`

Ten new compiler regressions cover the ten audited pairs. All ten compile against
the pre-fix layouts through a Go overlay; the final compiler suite rejects them
for the intended cannot-convert diagnostic. There are now 652 compiler cases.
Matcher regressions include interval and nested JSON inputs (nullable and required),
differing arities, nested slice/map substitutions and recursive equations. Each
positive probe also demonstrates that the old int-only sample misses it.
Catalogue mappings were not changed; these are additional safety regressions.
Existing old/new SQL golden coverage and PostgreSQL source coverage pass compilation.

All **95 footprint counts are unchanged**, with no new probes or ceiling changes:

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| q_with | 1211 | 1211 |
| aggregate_families | 4366 | 4369 |
| transaction_joins | 1835 | 2111 |
| transaction_correlation | 2908 | 3345 |

Files changed by this correction, including shared files:

- `database/orm/expr/aggregate.go`
- `database/orm/method_surface_test.go`
- `database/orm/phase_substitution_test.go`
- `database/orm/rows/transactions.md`
- `database/orm/testdata/public-methods.txt`
- `database/orm/transaction_phase_test.go`
- `database/query/field_json.go`
- `database/query/insert_mapping.go`
- `database/query/source_write_mapping.go`
- `database/query/unique_lookup.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/testdata/compilefail/expr_ordered_value_comparison_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/expr_ordered_value_comparison_ordered_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/expr_selected_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/expr_selected_ordered_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/expr_value_comparison_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/expr_value_comparison_ordered_aggregate_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_insert_update_mapping_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_json_ordered_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_lookup_reader_conversion/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/legacy_nullable_json_ordered_conversion/invalid.go`

The existing evidence JSON and this report are updated too. Lead-owned docs,
input carriers, budget definitions and catalogue rows are unchanged.

Acceptance used GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.

| Command | Result |
| --- | --- |
| go build ./... | PASS |
| go vet ./database/... ./internal/... | PASS |
| go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... | PASS |
| fixture: go vet ./... | PASS |
| fixture: go test ./... | PASS |
| python3 tools/goway_catalogue.py | PASS |
| repository gofmt check | PASS |
| git diff --check | PASS |
| make docs-check | PASS |
| 95 footprint counts before/after | PASS: identical |
| ten regressions against pre-fix layouts | PASS: reproduce conversions |

Logs: `/private/tmp/goway-6b4-audit-logs`. The inventory deliberately failed on
unclassified pairs before the allowlist audit was completed. No PostgreSQL was
run; its sources compiled and were vetted. No commits or Git writes were made.

### Docs notes

Lead changelog note: separate aggregate-only capabilities, legacy JSON ordering,
existence/model lookups and legacy insert/update mappings. The conversion guard
now matches nested/concrete generic substitutions and differing arities; input
carrier exceptions remain explicitly assigned to lane B's typed roles slice.
Public surface additions from both rebased sides are retained.

No questions for the lead. Full 6b4 patch relative to the new HEAD:
`/private/tmp/goway-6b4.patch`. Correction only, relative to the reapplied tree:
`/private/tmp/goway-6b4-audit-fix.patch`.
