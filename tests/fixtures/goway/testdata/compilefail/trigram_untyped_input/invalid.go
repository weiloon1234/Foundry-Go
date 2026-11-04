package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

type record struct{}

var _ = expr.TrigramMatch[record, string]("name", "word")
