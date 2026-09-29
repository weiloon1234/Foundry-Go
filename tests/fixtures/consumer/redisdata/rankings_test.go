package redisdata_test

import (
	"context"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

// structures records the encoded representations the typed handles select.
// Server ordering, bounds and atomicity are exercised by the native Redis suite.
type structures struct {
	recording
	data.SortedSetBackend
	data.ListBackend
	member, activity string
	increments       int64
}

func (b *structures) SortedSetIncrement(_ context.Context, _ data.Key, member string, delta float64, _ data.Limits) (float64, error) {
	b.member = member
	return delta, nil
}
func (b *structures) SortedSetRange(context.Context, data.Key, data.Window, data.Limits) ([]data.StoredMember, error) {
	return []data.StoredMember{{Member: b.member, Score: 1}}, nil
}
func (b *structures) ListTrim(context.Context, data.Key, int64, int64, data.Limits) error { return nil }
func (b *structures) ListPush(_ context.Context, _ data.Key, values []string, _ data.End, _ data.Limits) (uint64, error) {
	b.activity = values[0]
	return 1, nil
}
func (b *structures) HashIncrement(_ context.Context, _ data.Key, _ string, delta int64, _ data.Limits) (int64, error) {
	b.increments += delta
	return b.increments, nil
}

func TestRankingsKeepModelOwnedMembersAndCounters(t *testing.T) {
	b := &structures{}
	store, err := data.NewStore(b, data.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "rankings"}))
	if err != nil {
		t.Fatal(err)
	}
	rankings, err := redisdata.Rankings.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := redisdata.Recent.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := redisdata.Counts.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	group, err := model.NewID[models.Group]()
	if err != nil {
		t.Fatal(err)
	}
	member, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	if err := redisdata.RecordView(t.Context(), rankings, recent, counts, group, member); err != nil {
		t.Fatal(err)
	}
	top, err := redisdata.TopMembers(t.Context(), rankings, group, 3)
	if err != nil || len(top) != 1 || top[0].Member != member || top[0].Score != 1 {
		t.Fatal(top, err)
	}
	if b.activity == "" || b.increments != 1 {
		t.Fatal("activity or counter was not recorded", b.activity, b.increments)
	}
}
