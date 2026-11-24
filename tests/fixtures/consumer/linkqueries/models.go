// Package linkqueries exercises typed relation writes through pivot lifecycle.
package linkqueries

import (
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=link_members var=Members key=ID identity=false retrieval=memberRetrievalHooks
type Member struct {
	ID          model.ID[Member] `foundry:"member=ID,key-role=primary"`
	Name        string
	Alias       value.Nullable[string]
	DeletedAt   value.Nullable[time.Time] `foundry:"role=soft-delete"`
	Groups      relation.Through[Group, Membership]
	NamedGroups relation.Through[Group, NaturalLink]
	AliasGroups relation.Through[Group, NullableLink]
	Friends     relation.Through[Member, Friendship]
}

type GroupCode string

//foundry:model table=link_groups var=Groups key=Code identity=false retrieval=groupRetrievalHooks
type Group struct {
	Code      GroupCode
	Name      string
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
}

//foundry:model table=link_memberships var=Memberships key=ID identity=false hooks=membershipHooks
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member] `foundry:"role=column"`
	GroupCode GroupCode
	Role      string                    `foundry:"input=string"`
	CreatedAt time.Time                 `foundry:"role=created-at"`
	UpdatedAt temporal.DateTime         `foundry:"role=updated-at"`
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
}

func (Membership) MutateRole(role string) (string, error) {
	return strings.ToLower(strings.TrimSpace(role)), nil
}

//foundry:model table=link_natural var=NaturalLinks key=ID identity=false
type NaturalLink struct {
	ID         model.ID[NaturalLink]
	MemberName string
	GroupName  string
}

//foundry:model table=link_nullable var=NullableLinks key=ID identity=false
type NullableLink struct {
	ID          model.ID[NullableLink]
	MemberAlias value.Nullable[string]
	GroupName   value.Nullable[string]
}

//foundry:model table=link_friendships var=Friendships key=ID identity=false
type Friendship struct {
	ID     model.ID[Friendship]
	FromID model.ID[Member] `foundry:"role=column"`
	ToID   model.ID[Member] `foundry:"role=column"`
}
