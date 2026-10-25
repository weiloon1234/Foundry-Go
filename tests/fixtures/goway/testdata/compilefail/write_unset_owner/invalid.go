package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

type member struct{}
type other struct{}

func bad(w *orm.Write[member], field orm.Text[other, string]) { _ = orm.WriteUnset(w, field.Set) }
