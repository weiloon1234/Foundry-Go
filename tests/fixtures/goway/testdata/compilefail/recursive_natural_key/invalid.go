package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}
type other struct{}

var record = rows.ModelRecord(models.Users.Query())
var cte = rows.CTE("users_copy", record)
var source = rows.AsRecord[tag](cte, "u")
var ids = rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.At(rows.Scope(source), orm.KeyExpr(models.Users.ID)))))
var nullable = rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.At(rows.Scope(source), orm.NullableTextExpr(models.Users.Nickname)))))
var combined = rows.Union(record, record)
var tx = rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(models.Users.Query())))

type node struct{ Code string }
type nodes struct{ orm.Table[node, string] }

var tree = orm.Define("nodes", func(b orm.Builder[node], n *node) nodes { return nodes{orm.TableOf(b, &n.Code)} })

func invalid(self rows.RecursiveSelf[node]) {
	a := rows.AsRecord[tag](self, "a")
	_ = expr.Eq(expr.At(rows.Scope(a), orm.KeyExpr(tree.Key())), models.User{}.ID)
}
