// Package redisdata verifies typed Redis data contracts from a separate consumer.
package redisdata

import (
	"context"

	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

// doc-example:begin redis-data-1
type ProfileField string

const DisplayProfile ProfileField = "display"

type Profile struct {
	Email  DisplayEmail
	Labels []string
}
type Profiles = data.Hash[model.ID[Member], ProfileField, Profile]
type MemberGroups = data.Set[model.ID[Member], models.GroupCode]

var ProfileHashes = data.DefineHash[model.ID[Member], ProfileField, Profile]("member-profiles", 1, keyspace.TextKeys[model.ID[Member]](), keyspace.StringKeys[ProfileField]())
var Groups = data.DefineSet[model.ID[Member], models.GroupCode]("member-groups", 1, keyspace.TextKeys[model.ID[Member]]())

// doc-example:end redis-data-1

func SaveProfile(ctx context.Context, profiles Profiles, member mutatorqueries.Member) (bool, error) {
	email, err := member.AccessEmail()
	if err != nil {
		return false, err
	}
	return profiles.Set(ctx, member.ID, DisplayProfile, Profile{Email: email, Labels: []string{"active"}})
}
func ReadProfile(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member]) (Profile, bool, error) {
	return profiles.Get(ctx, id, DisplayProfile)
}
func AddGroup(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member], group models.GroupCode) (bool, error) {
	return groups.Add(ctx, id, group)
}
func ReadGroups(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member]) ([]models.GroupCode, error) {
	return groups.Members(ctx, id)
}
func ExpireGroups(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member], ttl cache.TTL) (bool, error) {
	return groups.Expire(ctx, id, ttl)
}
