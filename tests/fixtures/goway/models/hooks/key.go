package hooks

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// KeyedMember retains model-owned keys on restore and force deletion.
//
//foundry:model table=hook_keyed_members var=KeyedMembers key=ID identity=false
type KeyedMember struct {
	ID        model.ID[KeyedMember]
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
}
