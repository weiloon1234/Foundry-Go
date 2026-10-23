package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

type A struct{ ID int64 }
type B struct{ ID int64 }

func invalid(schema rows.Row[A]) { _, _ = rows.Raw[B](nil, nil, schema, "SELECT id") }
