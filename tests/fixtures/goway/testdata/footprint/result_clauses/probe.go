package result_clauses

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

func Grouped() orm.Query[models.User] {
	q := models.Users.Query()
	count := expr.Count[models.User]()
	scalar := rows.SelectScalar(rows.From(q), count.Value())
	scalar = rows.DistinctScalar(rows.HavingScalar(rows.GroupByScalar(scalar, expr.Group(expr.Value(orm.KeyExpr(models.Users.ID)))), count.Gt(0)))
	byValue := rows.DistinctOnScalarValues(scalar, count.Value().Key())
	return expr.Where(q, rows.ExistsQuery(rows.From(q), scalar), rows.ExistsQuery(rows.From(q), byValue))
}

func On() orm.Query[models.User] {
	q := models.Users.Query()
	scalar := rows.DistinctOnScalar(rows.SelectScalar(rows.From(q), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value()), expr.Group(expr.Value(orm.KeyExpr(models.Users.ID))))
	return expr.Where(q, rows.ExistsQuery(rows.From(q), scalar))
}

func Records() orm.Query[models.User] {
	q := models.Users.Query()
	record := rows.DistinctRecord(rows.SelectRecord(rows.From(q), rows.ModelScope(q)))
	combined := rows.DistinctSet(rows.UnionAll(record, record))
	return expr.Where(q, rows.ExistsQuery(rows.From(q), rows.AsRecord[alias](combined, "unique_people")))
}

func Values() orm.Query[models.User] {
	q := models.Users.Query()
	scalar := rows.Values(rows.SelectScalar(rows.From(q), expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))).Value()))
	combined := rows.DistinctValues(rows.UnionAllValues(scalar, scalar))
	return expr.Where(q, rows.ExistsQuery(rows.From(q), combined))
}
