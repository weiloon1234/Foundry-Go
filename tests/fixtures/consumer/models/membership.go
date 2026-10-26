package models

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type GroupCode string

type Group struct {
	Code    GroupCode
	Name    string
	OwnerID model.ID[User]
	Owner   relation.One[User]
}

type Membership struct {
	ID        string
	UserID    model.ID[User]
	GroupCode value.Nullable[GroupCode]
	Lookup    string
	Priority  int
	Level     Level
	InviterID value.Nullable[model.ID[User]]
	Inviter   relation.One[User]
}

type Friendship struct {
	ID     model.ID[Friendship]
	FromID model.ID[User]
	ToID   model.ID[User]
	Note   string
}
