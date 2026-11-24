package models

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type GroupCode string

//foundry:model table=groups var=Groups key=Code identity=false
type Group struct {
	Code    GroupCode `foundry:"member=Code,key-role=primary"`
	Name    string
	OwnerID model.ID[User]
	Owner   relation.One[User]
}

//foundry:model table=memberships var=Memberships key=ID identity=false
type Membership struct {
	ID        string `foundry:"member=ID,key-role=primary"`
	UserID    model.ID[User]
	GroupCode value.Nullable[GroupCode]
	Lookup    string
	Priority  int
	Level     Level
	InviterID value.Nullable[model.ID[User]]
	Inviter   relation.One[User]
}

//foundry:model table=friendships var=Friendships key=ID identity=false
type Friendship struct {
	ID     model.ID[Friendship] `foundry:"member=ID,key-role=primary"`
	FromID model.ID[User]
	ToID   model.ID[User]
	Note   string
}
