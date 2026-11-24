// Package retrievalqueries exercises ORM retrieval events and relations.
package retrievalqueries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=retrieval_members var=Members key=ID identity=false retrieval=memberRetrievalHooks
type Member struct {
	ID       model.ID[Member]
	Name     string `foundry:"input=string"`
	Nickname value.Nullable[string]
	ParentID value.Nullable[model.ID[Member]] `foundry:"role=column"`
	Parent   relation.One[Member]
	Children relation.Many[Member]
	Groups   relation.Through[Group, Membership]
}

func (Member) MutateName(name string) (string, error) {
	return strings.ToLower(strings.TrimSpace(name)), nil
}

func (member Member) AccessName() (string, error) { return strings.ToUpper(member.Name), nil }

type GroupCode string

//foundry:model table=retrieval_groups var=Groups key=Code identity=false
type Group struct {
	Code GroupCode
	Name string
}

//foundry:model table=retrieval_memberships var=Memberships key=ID identity=false
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member] `foundry:"role=column"`
	GroupCode value.Nullable[GroupCode]
}

type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}
