package write_unset

import "github.com/weiloon1234/Foundry-Go/database/orm"

type member struct{ Revision int64 }

func Remove(write *orm.Write[member], field orm.Number[member, int64]) error {
	return orm.WriteUnset(write, field.Set)
}
