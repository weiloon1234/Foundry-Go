# Trigram search

<a id="agent-entry"></a>
## Agent entry

- Search: [Transaction-local candidates](#recipe-trigram-search)

## Overview

`expr.TrigramMatch(text, search)` compiles to `text OPERATOR(public.%) $n`.
It accepts typed text row operands, including `expr.Value(orm.TextExpr(column))`
and joined `expr.At(scope, orm.TextExpr(column))` operands. The search keeps the
column's concrete `V ~string` type and codec and is bound as a parameter.
Use the returned predicate with `expr.Where` on models or `.Where` on a rows
projection. Declaration ownership, query budgets and transaction lock rules
remain enforced.

`TrigramMatch` does not accept nullable text, including columns on the nullable
side of a left join. An `IS NOT NULL` filter does not change the expression's Go
type. For a non-nullable stored column, filter its source before the left join
to retain unmatched left rows, or use an inner join when a match is required.
For nullable stored text, compare `TrigramSimilarityNullable` with
`expr.NullableGte`; this does not supply a GIN index condition. Alternatively,
use `expr.Coalesce` with an explicit text fallback before `TrigramMatch`.
That changes NULL semantics and may need an index on the same expression.

`expr.TrigramSimilarity(text, search)` compiles to
`public.similarity(text, $n)` and returns a selectable, comparable, orderable
`float64` expression. PostgreSQL computes the score as `real`; decoding into
`float64` does not add precision. `expr.TrigramSimilarityNullable` returns
`value.Nullable[float64]` and propagates NULL from the text operand.
`TrigramSimilarityValue` and `TrigramSimilarityNullableValue` accept selected
expressions and preserve aggregate/window phase, like spherical distance.

The extension schema is fixed to `public`. Applications install the extension
and indexes in their migrations; the framework does not install them:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
CREATE INDEX example_names_trgm_gin
    ON entity_names USING gin (normalized public.gin_trgm_ops);
CREATE INDEX example_reviews_trgm_gin
    ON reviews USING gin (body public.gin_trgm_ops);
```

These examples create full indexes. For a soft-delete table, consider a partial
GIN index with `WHERE deleted_at IS NULL` when the search includes that predicate.
Choose the index name and predicate to match the application's migration.

An existing extension must already be installed in `public`; `IF NOT EXISTS`
does not relocate it. The compiler qualifies both `%` and `similarity`, so it
does not depend on `public` being on the application's search path.

`SetTrigramThreshold` does not check that the extension is installed. Without
`pg_trgm`, `set_config` succeeds by creating a placeholder setting. The next
trigram query fails with SQLSTATE `42883`, classified as `query_failed`.
Install the extension before using the search API.

The `%` predicate can use a GIN trigram index on the searched column. A
`similarity()` comparison by itself does not provide that index condition;
ordering by similarity also does not become a GIN ordered scan. PostgreSQL
chooses the actual plan. Applying text calculations to the indexed column may
require a matching expression index. See PostgreSQL's
[pg_trgm reference](https://www.postgresql.org/docs/current/pgtrgm.html).

`expr.SetTrigramThreshold(ctx, tx, threshold)` requires a live `*database.Tx`.
It rejects nil/inactive transactions, NaN, infinity, values outside 0…1 and
positive subnormal `float64` values before I/O. The smallest accepted positive
value is `0x1p-1022`, about `2.2250738585072014e-308`. Negative zero is normalized
to zero. PostgreSQL's [real GUC parser](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/misc/guc.c)
uses a C `double` and rejects underflow; it is not the SQL `real` result type
used by `similarity()`. The helper executes
`SELECT set_config('pg_trgm.similarity_threshold', $1, true)` with the threshold
bound as text. The setting lasts until commit or rollback; the session's
previous value is restored before the connection is reused. A successful nested
savepoint retains the setting in the enclosing transaction; rolling back that
savepoint restores its previous setting.

Use a positive threshold. Zero is accepted, but matching results can depend on
the plan: a review probe on the same data returned one row with a GIN scan and
three with a sequential scan. Do not use threshold zero to request all rows.

The match predicate honours the threshold in that transaction. PostgreSQL's
default is **0.3**, unless the deployment overrides it. Combine the predicate
with a `TrigramSimilarity` comparison for a stricter or per-query cut-off; keep
the transaction's candidate threshold no higher than that cut-off. Run the helper
and search on the same transaction. See PostgreSQL's
[transaction-local settings](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-ADMIN-SET).

The [consumer search functions](../../tests/fixtures/consumer/trigramqueries/search.go)
contain both complete query shapes: `FindDuplicates` selects entity ID,
normalized name, score and exact decimal coordinates through two joins, filters
by country and excluded ID, orders by entity/name/address IDs and limits to
1001; `SimilarReviews` selects matching bodies, excludes one ID, orders by ID
and limits to 101. Exact decimal hydration replaces coordinate-to-text casts.
Applications can format those decimal values at their output boundary.

<a id="recipe-trigram-search"></a>
## Search within a transaction

<!-- compiled-source: tests/fixtures/consumer/trigramqueries/search.go#find-duplicates -->
```go
func FindDuplicates(ctx context.Context, tx *database.Tx, tables Tables, search Normalized, country string, except int64) ([]Duplicate, error) {
	if err := expr.SetTrigramThreshold(ctx, tx, 0.6); err != nil {
		return nil, err
	}
	n := rows.As[nameSide](tables.Names.Query(), "n")
	e := rows.As[entitySide](tables.Entities.Query(), "e")
	a := rows.As[addressSide](tables.Addresses.Query(), "a")
	names := rows.InnerJoin(n, e, rows.On(expr.At(rows.Scope(n), orm.NumberExpr(tables.Names.EntityID)), expr.At(rows.Scope(e), orm.KeyExpr(tables.Entities.Key()))))
	entity := rows.RightScope(names, rows.Scope(e))
	joined := rows.InnerJoin(names, a, rows.On(expr.At(entity, orm.KeyExpr(tables.Entities.Key())), expr.At(rows.Scope(a), orm.NumberExpr(tables.Addresses.EntityID))))
	nameScope := rows.LeftScope(joined, rows.LeftScope(names, rows.Scope(n)))
	entityScope := rows.LeftScope(joined, entity)
	addressScope := rows.RightScope(joined, rows.Scope(a))
	normalized := expr.At(nameScope, orm.TextExpr(tables.Names.Normalized))
	score := expr.TrigramSimilarity(normalized, search)
	entityID := expr.At(entityScope, orm.KeyExpr(tables.Entities.Key()))
	result := rows.Select(joined, Duplicates,
		rows.Map(Duplicates.ID, entityID),
		rows.Map(Duplicates.Normalized, normalized),
		rows.MapSelected(Duplicates.Similarity, expr.AsSelected(score)),
		rows.Map(Duplicates.Latitude, expr.At(addressScope, orm.NumberExpr(tables.Addresses.Latitude))),
		rows.Map(Duplicates.Longitude, expr.At(addressScope, orm.NumberExpr(tables.Addresses.Longitude))),
	).Where(
		expr.TrigramMatch(normalized, search), expr.Gte(score, 0.6),
		expr.Eq(expr.At(entityScope, orm.TextExpr(tables.Entities.Country)), country),
		expr.Ne(entityID, except),
	)
	result = rows.Limit(result, 1001)
	result = rows.OrderBy(result,
		expr.Asc(entityID),
		expr.Asc(expr.At(nameScope, orm.KeyExpr(tables.Names.Key()))),
		expr.Asc(expr.At(addressScope, orm.KeyExpr(tables.Addresses.Key()))),
	)
	return rows.Read(result).All(ctx, tx)
}
```

The second query reuses that transaction threshold.

<!-- compiled-source: tests/fixtures/consumer/trigramqueries/search.go#similar-reviews -->
```go
func SimilarReviews(ctx context.Context, tx *database.Tx, reviews ReviewTable, search string, except int64) ([]string, error) {
	body := expr.Value(orm.TextExpr(reviews.Body))
	query := expr.Where(reviews.Query().Where(orm.KeyNe(reviews.Key(), except)), expr.TrigramMatch(body, search))
	query = expr.OrderBy(query, expr.Asc(expr.Value(orm.KeyExpr(reviews.Key())))).Limit(101)
	return rows.ReadScalar(rows.SelectScalar(rows.From(query), expr.AsSelected(expr.Row(body)))).All(ctx, tx)
}
```

The [PostgreSQL acceptance source](../../tests/fixtures/consumer/trigramqueries/search_postgres_test.go)
uses `FOUNDRY_TEST_POSTGRES_URL` and an isolated schema with
[passing-test cleanup](contributing.md#postgresql-acceptance). It covers both
searches, ordering, NULLs, locks, threshold range validation, savepoint release
and rollback, threshold restoration on the same backend and a GIN plan with `enable_seqscan=off`.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
