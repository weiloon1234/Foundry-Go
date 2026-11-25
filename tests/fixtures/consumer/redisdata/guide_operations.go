package redisdata

import (
	"context"
	"math"
	"time"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

// doc-example:begin redis-data-1
func bindStructures(client *redis.Client, namespace keyspace.Namespace) error {
	store, err := data.NewStore(client, data.DefaultConfig(namespace))
	if err != nil {
		return err
	}
	profiles, err := ProfileHashes.Bind(store)
	if err != nil {
		return err
	}
	groups, err := Groups.Bind(store)
	if err != nil {
		return err
	}
	_ = profiles
	_ = groups
	return err
}

// doc-example:end redis-data-1

// doc-example:begin redis-data-2
func profileFields(ctx context.Context, member Member, profiles Profiles) error {
	email, err := member.AccessEmail()
	if err != nil {
		return err
	}
	added, err := profiles.Set(ctx, member.ID, DisplayProfile,
		Profile{Email: email, Labels: []string{"active"}})
	if err != nil {
		return err
	}
	profile, found, err := profiles.Get(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	removed, err := profiles.DeleteField(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	count, err := profiles.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	_ = added
	_ = profile
	_ = found
	_ = removed
	_ = count
	return err
}

// doc-example:end redis-data-2

// doc-example:begin redis-data-3
func groupMembers(ctx context.Context, member Member, groupID model.ID[models.Group], groups MemberGroups) error {
	added, err := groups.Add(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	present, err := groups.Contains(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	members, err := groups.Members(ctx, member.ID)
	if err != nil {
		return err
	}
	removed, err := groups.Remove(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	count, err := groups.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	_ = added
	_ = present
	_ = members
	_ = removed
	_ = count
	return err
}

// doc-example:end redis-data-3

// doc-example:begin redis-data-4
func manyProfiles(ctx context.Context, member Member, profiles Profiles) error {
	values, err := profiles.GetMany(ctx, member.ID, DisplayProfile /*, more fields */)
	if err != nil {
		return err
	}
	display, found := values[0].Get() // value.Optional[Profile], in input order
	_ = display
	_ = found
	return err
}

// doc-example:end redis-data-4

// doc-example:begin redis-data-5
func activityCounts(ctx context.Context, member Member, counts ActivityCounts) error {
	entries, err := counts.GetAll(ctx, member.ID)
	if err != nil {
		return err
	}
	total, err := data.IncrementField(ctx, counts, member.ID, PageViews, 1)
	if err != nil {
		return err
	}
	_ = entries
	_ = total
	return err
}

// doc-example:end redis-data-5

// doc-example:begin redis-data-6
func rankedMembers(ctx context.Context, member Member, store *data.Store, groupID model.ID[models.Group]) error {
	rankings, err := Rankings.Bind(store)
	if err != nil {
		return err
	}
	added, err := rankings.Add(ctx, groupID, member.ID, 1250)
	if err != nil {
		return err
	}
	score, err := rankings.Increment(ctx, groupID, member.ID, 25)
	if err != nil {
		return err
	}
	top, err := rankings.Range(ctx, groupID, data.Last(10)) // []data.Scored[model.ID[Member]]
	if err != nil {
		return err
	}
	rank, found, err := rankings.Rank(ctx, groupID, member.ID, data.Descending)
	if err != nil {
		return err
	}
	window, err := rankings.RangeByScore(ctx, groupID, data.Between(1000, 2000), data.First(20))
	if err != nil {
		return err
	}
	count, err := rankings.CountByScore(ctx, groupID, data.ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)})
	if err != nil {
		return err
	}
	_ = added
	_ = score
	_ = top
	_ = rank
	_ = found
	_ = window
	_ = count
	return err
}

// doc-example:end redis-data-6

// doc-example:begin redis-data-7
func recentActivity(ctx context.Context, member Member, store *data.Store, groupID model.ID[models.Group], newest Activity) error {
	recent, err := Recent.Bind(store)
	if err != nil {
		return err
	}
	err = recent.Trim(ctx, groupID, -99, -1) // keep the newest 99 before pushing
	if err != nil {
		return err
	}
	length, err := recent.Push(ctx, groupID, Activity{Member: member.ID, Kind: PageViews})
	if err != nil {
		return err
	}
	length, err = recent.PushFront(ctx, groupID, newest)
	if err != nil {
		return err
	}
	items, err := recent.Range(ctx, groupID, 0, -1)
	if err != nil {
		return err
	}
	item, found, err := recent.PopFront(ctx, groupID)
	if err != nil {
		return err
	}
	_ = length
	_ = items
	_ = item
	_ = found
	return err
}

// doc-example:end redis-data-7

// doc-example:begin redis-data-8
func groupLifetime(ctx context.Context, member Member, groups MemberGroups, firstMember, secondMember Member) error {
	exists, err := groups.Exists(ctx, member.ID)
	if err != nil {
		return err
	}
	changed, err := groups.Expire(ctx, member.ID, cache.For(time.Hour))
	if err != nil {
		return err
	}
	changed, err = groups.Expire(ctx, member.ID, cache.Forever())
	if err != nil {
		return err
	}
	deleted, err := groups.DeleteMany(ctx, firstMember.ID, secondMember.ID)
	if err != nil {
		return err
	}
	_ = exists
	_ = changed
	_ = deleted
	return err
}

// doc-example:end redis-data-8
