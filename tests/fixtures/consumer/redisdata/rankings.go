package redisdata

import (
	"context"

	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

type GroupRanking = data.SortedSet[model.ID[models.Group], model.ID[mutatorqueries.Member]]
type RecentActivity = data.List[model.ID[models.Group], Activity]

// doc-example:begin redis-data-6
var Rankings = data.DefineSortedSet[model.ID[models.Group], model.ID[Member]]("group-rankings", 1, keyspace.TextKeys[model.ID[models.Group]]())

// doc-example:end redis-data-6

// doc-example:begin redis-data-7
var Recent = data.DefineList[model.ID[models.Group], Activity]("group-activity", 1, keyspace.TextKeys[model.ID[models.Group]]())

// doc-example:end redis-data-7

// doc-example:begin redis-data-5
var Counts = data.DefineHash[model.ID[Member], ActivityKind, int64]("member-activity", 1, keyspace.TextKeys[model.ID[Member]](), keyspace.StringKeys[ActivityKind]()).
	WithFieldDecoder(func(text string) (ActivityKind, error) { return ActivityKind(text), nil })

// doc-example:end redis-data-5

// RecordView updates the three structures for one page view.
func RecordView(ctx context.Context, rankings GroupRanking, recent RecentActivity, counts ActivityCounts, group model.ID[models.Group], member model.ID[mutatorqueries.Member]) error {
	if _, err := rankings.Increment(ctx, group, member, 1); err != nil {
		return err
	}
	// Keep a capped window: trim to the newest entries before pushing.
	if err := recent.Trim(ctx, group, -99, -1); err != nil {
		return err
	}
	if _, err := recent.Push(ctx, group, Activity{Member: member, Kind: PageViews}); err != nil {
		return err
	}
	_, err := data.IncrementField(ctx, counts, member, PageViews, 1)
	return err
}

// TopMembers returns the highest-scored members of a group.
func TopMembers(ctx context.Context, rankings GroupRanking, group model.ID[models.Group], count int) ([]data.Scored[model.ID[mutatorqueries.Member]], error) {
	return rankings.Range(ctx, group, data.Last(count))
}

// MemberActivity returns every typed activity counter of a member.
func MemberActivity(ctx context.Context, counts ActivityCounts, member model.ID[mutatorqueries.Member]) ([]data.HashEntry[ActivityKind, int64], error) {
	return counts.GetAll(ctx, member)
}

// doc-example:begin redis-activity-types
type ActivityKind string

const PageViews ActivityKind = "page-views"

// Activity is a consumer-owned DTO stored in a bounded recent-activity list.
type Activity struct {
	Member model.ID[Member]
	Kind   ActivityKind
}
type ActivityCounts = data.Hash[model.ID[Member], ActivityKind, int64]

// doc-example:end redis-activity-types
