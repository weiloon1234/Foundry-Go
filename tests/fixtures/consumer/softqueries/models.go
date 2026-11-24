// Package softqueries exercises soft-delete lifecycle and relation visibility.
package softqueries

import (
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=soft_members var=Members key=ID hooks=ormMemberHooks identity=false
type Member struct {
	ID        model.ID[Member]
	Name      string                           `foundry:"input=string"`
	ParentID  value.Nullable[model.ID[Member]] `foundry:"role=column"`
	CreatedAt time.Time                        `foundry:"role=created-at"`
	UpdatedAt temporal.DateTime                `foundry:"role=updated-at"`
	// DeletedAt stores the row visibility timestamp in removed_on.
	// Managed soft-delete timestamp: Delete sets it and Restore clears it.
	// Custom getter: AccessDeletedAt reports whether the stored timestamp is present.
	DeletedAt  value.Nullable[time.Time] `foundry:"column=removed_on,role=soft-delete"`
	Parent     relation.One[Member]
	Children   relation.Many[Member]
	Groups     relation.Through[Group, Membership]
	ChildCount relation.Value[int64]
	GroupCount relation.Value[int64]
}

func (Member) MutateName(name string) (string, error) {
	return strings.ToLower(strings.TrimSpace(name)), nil
}
func (m Member) AccessDeletedAt() (bool, error) { return !m.DeletedAt.IsNull(), nil }

type GroupCode string

//foundry:model table=soft_groups var=Groups key=Code identity=false
type Group struct {
	Code GroupCode
	// Name selects the stored group name. Example: orm.Eq(Groups.Name, "reserved").
	Name string
	// DeletedAt controls soft-delete visibility. Delete sets it and Restore clears it.
	// Example: Groups.Query().WithTrashed().
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
}

//foundry:model table=soft_memberships var=Memberships key=ID identity=false
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member] `foundry:"role=column"`
	GroupCode GroupCode
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
}

type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}
