package consumer

import o "github.com/weiloon1234/Foundry-Go/database/orm"
import "github.com/weiloon1234/Foundry-Go/database/orm/expr"
import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

var c o.Text[int, string]
var input o.InputText[int, int, string]
var q o.Query[int]
var _ = expr.Value[int, string](o.TextExpr(c))
var _ = expr.GreaterColumn(o.TextExpr(c), o.TextExpr(c))
var _ = expr.At(1, o.TextExpr(c))
var _ = rows.SelectValue(q, o.TextExpr(c))
var _ = rows.MapInsert(o.TextExpr(c), "value")
var _ = rows.MapUpdate(o.TextExpr(c), "value")
