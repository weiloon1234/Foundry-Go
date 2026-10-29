package consumer

import o "github.com/weiloon1234/Foundry-Go/database/orm"
import "github.com/weiloon1234/Foundry-Go/database/orm/expr"
import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

var c o.Text[int, string]
var input o.InputText[int, int, string]
var q o.Query[int]
var _ = expr.Value[int, string](c)
var _ = expr.GreaterColumn(c, c)
var _ = expr.At(1, c)
var _ = rows.SelectValue(q, c)
var _ = rows.MapInsert(c, "value")
var _ = rows.MapUpdate(c, "value")
