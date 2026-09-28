// Package softqueries exercises soft-delete lifecycle and relation visibility.
package softqueries

import (
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=soft_members hooks=memberHooks
type Member struct {
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Name retains stored soft_members.name. Custom setter: [Member.MutateName] transforms assigned values during persistence through [MemberDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name     string
	ParentID value.Nullable[model.ID[Member]]
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	// Foundry field behavior (generated): Member.DeletedAt retains stored soft_members.removed_on. Custom getter: [Member.AccessDeletedAt]; choose the stored field or getter result explicitly when mapping a DTO.
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

//foundry:model table=soft_groups primary=Code
type Group struct {
	Code GroupCode
	Name string
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
}

//foundry:model table=soft_memberships
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member]
	GroupCode GroupCode
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[time.Time]
}

func (Member) DefineRelations() MemberRelationSet {
	m, g, p := MemberFields(), GroupFields(), MembershipFields()
	return MemberRelationSet{
		Parent:   query.BelongsTo(m.ParentID, m.ID),
		Children: query.HasMany(m.ID, m.ParentID),
		Groups:   query.ManyToMany(m.ID, p.MemberID, p.GroupCode, g.Code),
	}
}

func (Member) DefineAggregates() MemberAggregateSet {
	r := MemberRelations()
	return MemberAggregateSet{
		ChildCount: query.Related(r.Children, query.Count[Member]()),
		GroupCount: query.Related(r.Groups, query.Count[Group]()),
	}
}

//foundry:projection
type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}
