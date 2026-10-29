package on_comparisons

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type aTag struct{}
type bTag struct{}

func F() rows.LeftJoined[rows.Alias[aTag, models.User], rows.Alias[bTag, models.User]] {
	a, b := rows.As[aTag](models.Users.Query(), "a"), rows.As[bTag](models.Users.Query(), "b")
	l, r := expr.At(rows.Scope(a), orm.NumberExpr(models.Users.Age)), expr.At(rows.Scope(b), orm.NumberExpr(models.Users.Age))
	on := rows.OnAnd(rows.OnEqual(l, r), rows.OnOr(rows.OnLess(expr.Add(l, l), expr.Add(r, r)), rows.OnDistinctFrom(l, r)).Not()).WhereLeft(l.Ne(0)).WhereRight(r.Ne(0))
	return rows.LeftJoin(a, b, on)
}
