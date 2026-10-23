package hooks

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// KeyedMember retains model-owned keys on restore and force deletion.
type KeyedMember struct {
	ID        model.ID[KeyedMember]
	DeletedAt value.Nullable[time.Time]
}
type KeyedMemberTable struct {
	orm.Table[KeyedMember, model.ID[KeyedMember]]
	DeletedAt orm.NullableTime[KeyedMember, time.Time]
}

var KeyedMembers = orm.Define("hook_keyed_members", func(b orm.Builder[KeyedMember], m *KeyedMember) KeyedMemberTable {
	return KeyedMemberTable{orm.TableOf(b, &m.ID), orm.NullableTimeOf(b, &m.DeletedAt)}
})
