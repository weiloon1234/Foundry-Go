package redisdata

import (
	"context"
	"math"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

func bindStructures(client *redis.Client, namespace keyspace.Namespace) (Profiles, MemberGroups, error) {
	// doc-example:begin redis-data-1
	store, err := data.NewStore(client, data.DefaultConfig(namespace))
	if err != nil {
		return Profiles{}, MemberGroups{}, err
	}
	profiles, err := ProfileHashes.Bind(store)
	if err != nil {
		return Profiles{}, MemberGroups{}, err
	}
	groups, err := Groups.Bind(store)
	if err != nil {
		return Profiles{}, MemberGroups{}, err
	}
	// doc-example:end redis-data-1
	return profiles, groups, nil
}

func profileFields(ctx context.Context, member Member, profiles Profiles) error {
	// doc-example:begin redis-data-2
	email, err := member.AccessEmail()
	if err != nil {
		return err
	}
	added, err := profiles.Set(ctx, member.ID, DisplayProfile,
		Profile{Email: email, Labels: []string{"active"}})
	if err != nil {
		return err
	}
	// doc-example:end redis-data-2
	// doc-example:begin redis-data-2-read
	profile, found, err := profiles.Get(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-2-read
	// doc-example:begin redis-data-2-delete
	removed, err := profiles.DeleteField(ctx, member.ID, DisplayProfile)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-2-delete
	// doc-example:begin redis-data-2-count
	count, err := profiles.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-2-count
	_ = added
	_ = profile
	_ = found
	_ = removed
	_ = count
	return err
}

func groupMembers(ctx context.Context, member Member, groupID models.GroupCode, groups MemberGroups) error {
	// doc-example:begin redis-data-3
	added, err := groups.Add(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-3
	// doc-example:begin redis-data-3-contains
	present, err := groups.Contains(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-3-contains
	// doc-example:begin redis-data-3-members
	members, err := groups.Members(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-3-members
	// doc-example:begin redis-data-3-remove
	removed, err := groups.Remove(ctx, member.ID, groupID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-3-remove
	// doc-example:begin redis-data-3-count
	count, err := groups.Count(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-3-count
	_ = added
	_ = present
	_ = members
	_ = removed
	_ = count
	return err
}

func manyProfiles(ctx context.Context, member Member, profiles Profiles) error {
	// doc-example:begin redis-data-4
	values, err := profiles.GetMany(ctx, member.ID, DisplayProfile /*, more fields */)
	if err != nil {
		return err
	}
	display, found := values[0].Get() // value.Optional[Profile], in input order
	// doc-example:end redis-data-4
	_ = display
	_ = found
	return err
}

func activityCounts(ctx context.Context, member Member, counts ActivityCounts) error {
	// doc-example:begin redis-data-5
	entries, err := counts.GetAll(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-5
	// doc-example:begin redis-data-5-increment
	total, err := data.IncrementField(ctx, counts, member.ID, PageViews, 1)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-5-increment
	_ = entries
	_ = total
	return err
}

func rankedMembers(ctx context.Context, member Member, store *data.Store, groupID models.GroupCode) error {
	// doc-example:begin redis-data-6
	rankings, err := Rankings.Bind(store)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6
	// doc-example:begin redis-data-6-add
	_, err = rankings.Add(ctx, groupID, member.ID, 1250) // first result: true when the member is new
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-add
	// doc-example:begin redis-data-6-increment
	_, err = rankings.Increment(ctx, groupID, member.ID, 25) // first result: the new score
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-increment
	// doc-example:begin redis-data-6-top
	top, err := rankings.Range(ctx, groupID, data.Last(10)) // []data.Scored[model.ID[Member]]
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-top
	// doc-example:begin redis-data-6-rank
	_, _, err = rankings.Rank(ctx, groupID, member.ID, data.Descending) // zero-based position, then whether the member is ranked
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-rank
	// doc-example:begin redis-data-6-window
	window, err := rankings.RangeByScore(ctx, groupID, data.Between(1000, 2000), data.First(20))
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-window
	// doc-example:begin redis-data-6-count
	count, err := rankings.CountByScore(ctx, groupID, data.ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)})
	if err != nil {
		return err
	}
	// doc-example:end redis-data-6-count
	_ = top
	_ = window
	_ = count
	return err
}

func recentActivity(ctx context.Context, member Member, store *data.Store, groupID models.GroupCode, newest Activity) error {
	// doc-example:begin redis-data-7
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
	// doc-example:end redis-data-7
	// doc-example:begin redis-data-7-prepend
	length, err = recent.PushFront(ctx, groupID, newest)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-7-prepend
	// doc-example:begin redis-data-7-read
	items, err := recent.Range(ctx, groupID, 0, -1)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-7-read
	// doc-example:begin redis-data-7-pop
	item, found, err := recent.PopFront(ctx, groupID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-7-pop
	_ = length
	_ = items
	_ = item
	_ = found
	return err
}

func groupLifetime(ctx context.Context, member Member, groups MemberGroups, firstMember, secondMember Member) error {
	// doc-example:begin redis-data-8
	exists, err := groups.Exists(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-8
	// doc-example:begin redis-data-8-expire
	changed, err := groups.Expire(ctx, member.ID, cache.For(time.Hour))
	if err != nil {
		return err
	}
	// doc-example:end redis-data-8-expire
	// doc-example:begin redis-data-8-persist
	changed, err = groups.Expire(ctx, member.ID, cache.Forever())
	if err != nil {
		return err
	}
	// doc-example:end redis-data-8-persist
	// doc-example:begin redis-data-8-delete
	deleted, err := groups.DeleteMany(ctx, firstMember.ID, secondMember.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-data-8-delete
	_ = exists
	_ = changed
	_ = deleted
	return err
}
