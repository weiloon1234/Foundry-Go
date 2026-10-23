package lifecycle_changes

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Compare(before, after h.Member) (bool, error) {
	changes, err := orm.Compare(h.Members.Table, value.Set(before), value.Set(after), h.Members.Name.Set("same"))
	if err != nil {
		return false, err
	}
	name, err := orm.ChangeOf(changes, h.Members.Name)
	if err != nil {
		return false, err
	}
	note, err := orm.NullableChangeOf(changes, h.Members.Note)
	return name.Assigned() || note.Changed(), err
}
