// Package retrievalqueries exercises generated retrieval events and relations.
package retrievalqueries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=retrieval_members retrieval=memberRetrieval
type Member struct {
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Name retains stored retrieval_members.name. Custom getter: [Member.AccessName]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Name retains stored retrieval_members.name. Custom setter: [Member.MutateName] transforms assigned values during persistence through [MemberDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name     string
	Nickname value.Nullable[string]
	ParentID value.Nullable[model.ID[Member]]
	Parent   relation.One[Member]
	Children relation.Many[Member]
	Groups   relation.Through[Group, Membership]
}

func (Member) MutateName(name string) (string, error) {
	return strings.ToLower(strings.TrimSpace(name)), nil
}

func (member Member) AccessName() (string, error) { return strings.ToUpper(member.Name), nil }

type GroupCode string

//foundry:model table=retrieval_groups primary=Code
type Group struct {
	Code GroupCode
	Name string
}

//foundry:model table=retrieval_memberships
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member]
	GroupCode value.Nullable[GroupCode]
}

//foundry:projection
type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}

func (Member) DefineRelations() MemberRelationSet {
	m, g, p := MemberFields(), GroupFields(), MembershipFields()
	return MemberRelationSet{
		Parent:   query.BelongsTo(m.ParentID, m.ID),
		Children: query.HasMany(m.ID, m.ParentID),
		Groups:   query.ManyToMany(m.ID, p.MemberID, p.GroupCode, g.Code),
	}
}
