package trigramqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type nameSide struct{}
type entitySide struct{}
type addressSide struct{}

// FindDuplicates reproduces the bounded name/entity/address search. Exact decimal
// hydration replaces the application's latitude::text and longitude::text casts.
// doc-example:begin find-duplicates
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
} // doc-example:end find-duplicates

// SimilarReviews uses the threshold already set in the supplied transaction.
// doc-example:begin similar-reviews
func SimilarReviews(ctx context.Context, tx *database.Tx, reviews ReviewTable, search string, except int64) ([]string, error) {
	body := expr.Value(orm.TextExpr(reviews.Body))
	query := expr.Where(reviews.Query().Where(orm.KeyNe(reviews.Key(), except)), expr.TrigramMatch(body, search))
	query = expr.OrderBy(query, expr.Asc(expr.Value(orm.KeyExpr(reviews.Key())))).Limit(101)
	return rows.ReadScalar(rows.SelectScalar(rows.From(query), expr.AsSelected(expr.Row(body)))).All(ctx, tx)
} // doc-example:end similar-reviews
