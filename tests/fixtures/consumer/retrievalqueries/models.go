// Package retrievalqueries exercises ORM retrieval events and relations.
package retrievalqueries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct {
	ID       model.ID[Member]
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

type Group struct {
	Code GroupCode
	Name string
}

type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member]
	GroupCode value.Nullable[GroupCode]
}

type MemberLabel struct {
	ID   model.ID[Member]
	Name string
}
