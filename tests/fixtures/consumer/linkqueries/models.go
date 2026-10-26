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

type Member struct {
	ID          model.ID[Member]
	Name        string
	Alias       value.Nullable[string]
	DeletedAt   value.Nullable[time.Time]
	Groups      relation.Through[Group, Membership]
	NamedGroups relation.Through[Group, NaturalLink]
	AliasGroups relation.Through[Group, NullableLink]
	Friends     relation.Through[Member, Friendship]
}

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
	Role      string
	CreatedAt time.Time
	UpdatedAt temporal.DateTime
	DeletedAt value.Nullable[time.Time]
}

func (Membership) MutateRole(role string) (string, error) {
	return strings.ToLower(strings.TrimSpace(role)), nil
}

type NaturalLink struct {
	ID         model.ID[NaturalLink]
	MemberName string
	GroupName  string
}

type NullableLink struct {
	ID          model.ID[NullableLink]
	MemberAlias value.Nullable[string]
	GroupName   value.Nullable[string]
}

type Friendship struct {
	ID     model.ID[Friendship]
	FromID model.ID[Member]
	ToID   model.ID[Member]
}
