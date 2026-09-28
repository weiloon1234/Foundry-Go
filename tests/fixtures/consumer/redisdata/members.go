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

type ProfileField string

const DisplayProfile ProfileField = "display"

type Profile struct {
	Email  mutatorqueries.DisplayEmail
	Labels []string
}
type Profiles = data.Hash[model.ID[mutatorqueries.Member], ProfileField, Profile]
type MemberGroups = data.Set[model.ID[mutatorqueries.Member], model.ID[models.Group]]

var ProfileHashes = data.DefineHash[model.ID[mutatorqueries.Member], ProfileField, Profile]("member-profiles", 1, keyspace.TextKeys[model.ID[mutatorqueries.Member]](), keyspace.StringKeys[ProfileField]())
var Groups = data.DefineSet[model.ID[mutatorqueries.Member], model.ID[models.Group]]("member-groups", 1, keyspace.TextKeys[model.ID[mutatorqueries.Member]]())

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
func AddGroup(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member], group model.ID[models.Group]) (bool, error) {
	return groups.Add(ctx, id, group)
}
func ReadGroups(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member]) ([]model.ID[models.Group], error) {
	return groups.Members(ctx, id)
}
func ExpireGroups(ctx context.Context, groups MemberGroups, id model.ID[mutatorqueries.Member], ttl cache.TTL) (bool, error) {
	return groups.Expire(ctx, id, ttl)
}
