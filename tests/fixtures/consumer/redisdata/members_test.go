package redisdata_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

// A focused recording adapter verifies the consumer's selected representations.
// Server atomicity and expiry are exercised by the native Redis integration suite.
type recording struct {
	data.Backend
	data.HashBackend
	data.SetBackend
	profile, group string
}

func (b *recording) HashSet(_ context.Context, _ data.Key, _ string, text string, _ data.Limits) (bool, error) {
	b.profile = text
	return true, nil
}
func (b *recording) HashGet(context.Context, data.Key, string, data.Limits) (string, bool, error) {
	return b.profile, true, nil
}
func (b *recording) SetAdd(_ context.Context, _ data.Key, text string, _ data.Limits) (bool, error) {
	b.group = text
	return true, nil
}
func (b *recording) SetMembers(context.Context, data.Key, data.Limits) ([]string, error) {
	return []string{b.group}, nil
}
func TestDataConsumerRetainsGettersAndModelOwnership(t *testing.T) {
	b := &recording{}
	store, err := data.NewStore(b, data.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "data"}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := redisdata.ProfileHashes.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := redisdata.Groups.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	group, err := model.NewID[models.Group]()
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "raw@example.test"}
	if _, err := redisdata.SaveProfile(t.Context(), profiles, member); err != nil {
		t.Fatal(err)
	}
	profile, hit, err := redisdata.ReadProfile(t.Context(), profiles, id)
	want, getterErr := member.AccessEmail()
	if err != nil || getterErr != nil || !hit || profile.Email != want || member.Email != "raw@example.test" {
		t.Fatal(profile, hit, err, getterErr)
	}
	profile.Labels[0] = "changed"
	again, _, err := redisdata.ReadProfile(t.Context(), profiles, id)
	if err != nil || again.Labels[0] != "active" {
		t.Fatal(again, err)
	}
	if _, err := redisdata.AddGroup(t.Context(), groups, id, group); err != nil {
		t.Fatal(err)
	}
	got, err := redisdata.ReadGroups(t.Context(), groups, id)
	if err != nil || len(got) != 1 || got[0] != group {
		t.Fatal(got, err)
	}
}
func TestDataConsumerAssemblyDoesNotConnect(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	client, err := redis.Prepare(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	store, err := data.NewStore(client, data.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "data"}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := redisdata.Groups.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := redisdata.ReadGroups(t.Context(), groups, id); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if stats := client.Stats(); stats.Open != 0 || stats.Ready || stats.Operations != 0 {
		t.Fatal(stats)
	}
}
