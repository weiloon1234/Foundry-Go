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

type Member struct {
	ID        model.ID[Member]
	Name      string
	ParentID  value.Nullable[model.ID[Member]]
	CreatedAt time.Time
	UpdatedAt temporal.DateTime
	// DeletedAt stores the row visibility timestamp in removed_on.
	// Managed soft-delete timestamp: Delete sets it and Restore clears it.
	// Custom getter: AccessDeletedAt reports whether the stored timestamp is present.
	DeletedAt  value.Nullable[time.Time] `foundry:"column=removed_on"`
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

type Group struct {
	Code      GroupCode
	Name      string
	DeletedAt value.Nullable[temporal.DateTime]
}

type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member]
	GroupCode GroupCode
	DeletedAt value.Nullable[time.Time]
}

type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}
