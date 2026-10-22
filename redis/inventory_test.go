package redis_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/secret"
)

var inventoryLimits = foundation.NewKey[*ratelimit.Store]("app.limits")
var inventoryExtraLimits = foundation.NewKey[*ratelimit.Store]("app.extra-limits")
var inventoryLockout = foundation.NewKey[*lockout.Store]("app.lockout")

func inventoryOfflineConnection() infrastructure.RedisConnectionSettings {
	connection := infrastructure.DefaultRedisConnectionSettings()
	connection.Host = "private-redis.invalid"
	return connection
}

func inventorySettings(namespace keyspace.Namespace, connection infrastructure.RedisConnectionSettings) (infrastructure.Settings, []infrastructure.RedisStore) {
	s := infrastructure.DefaultSettings()
	s.Namespace = namespace
	s.Redis.Connections = infrastructure.RedisConnections{"default": connection, "second": connection}
	job := infrastructure.DefaultJobConnectionSettings()
	job.Driver = infrastructure.RedisJobs
	secondJob := job
	secondJob.Redis = "second"
	s.Jobs.Connections = infrastructure.JobConnections{"default": job, "mail": secondJob}
	s.Coordination.Enabled = true
	s.Coordination.Driver = infrastructure.RedisCoordination
	c := infrastructure.DefaultCacheSettings()
	c.Driver = infrastructure.RedisCache
	c.Require = cache.Requirements{Tags: true, Batches: true, DistributedFills: true}
	secondCache := c
	secondCache.Redis = "second"
	secondCache.Config.Namespace = namespace
	secondCache.Config.Namespace.Application += ".override"
	s.Cache.Stores = infrastructure.CacheStores{"default": c, "second": secondCache}
	realtime := infrastructure.DefaultRealtimeConnectionSettings()
	realtime.Driver = infrastructure.RedisRealtime
	s.Realtime.Connections = infrastructure.RealtimeConnections{"default": realtime}
	broker := infrastructure.DefaultBrokerSettings()
	broker.Driver = infrastructure.RedisBroker
	s.PubSub.Connections = infrastructure.Brokers{"default": broker}
	extra := namespace
	extra.Application += ".limits"
	return s, []infrastructure.RedisStore{
		infrastructure.RedisRateLimitStore(inventoryLimits, "", ratelimit.DefaultConfig(keyspace.Namespace{})),
		infrastructure.RedisRateLimitStore(inventoryExtraLimits, "second", ratelimit.DefaultConfig(extra)),
		infrastructure.RedisLockoutStore(inventoryLockout, "second", lockout.DefaultConfig(keyspace.Namespace{})),
	}
}

func TestRedisInventoryCompleteDeterministicAndSecretFree(t *testing.T) {
	connection := inventoryOfflineConnection()
	connection.User = "private-user"
	connection.Password = secret.New("redis://private-user:private-password@private-redis.invalid")
	s, declarations := inventorySettings(keyspace.Namespace{Application: "inventory", Environment: "production"}, connection)
	option := infrastructure.WithRedisStores(declarations...)
	declarations[0] = nil // The option owns its declaration slice.
	plan, err := infrastructure.Configure(s, option)
	if err != nil {
		t.Fatal(err)
	}
	inventory := plan.RedisKeys()
	if len(inventory) != 2 || inventory[0].Connection != "default" || inventory[1].Connection != "second" {
		t.Fatal(inventory)
	}
	want := [][]string{
		{"cache:inventory.default", "cache-namespace:inventory.default", "jobs:inventory.jobs.default", "lease:inventory.coordination", "lease:inventory.default", "ratelimit:inventory", "websocket:inventory.realtime.default"},
		{"cache:inventory.override", "cache-namespace:inventory.override", "jobs:inventory.jobs.mail", "lease:inventory.override", "lockout:inventory", "ratelimit:inventory.limits"},
	}
	for i, connection := range inventory {
		got := []string{}
		prefixes := []string{}
		for _, entry := range connection.Keys {
			got = append(got, string(entry.Feature)+":"+entry.Namespace.Application)
			prefixes = append(prefixes, entry.Prefix)
			if entry.Version != "v1" || entry.Namespace.Environment != "production" || entry.Prefix != "foundry:"+string(entry.Feature)+":v1:"+entry.Namespace.Application+":production:" {
				t.Fatal(entry)
			}
		}
		slices.Sort(got)
		slices.Sort(want[i])
		if !reflect.DeepEqual(got, want[i]) || !slices.IsSorted(prefixes) {
			t.Fatalf("keys: %v want %v", got, want[i])
		}
	}
	if len(inventory[0].Channels) != 2 || len(inventory[1].Channels) != 0 {
		t.Fatal("channel requirements were lost or became keys")
	}
	for _, entry := range inventory[0].Channels {
		if entry.Feature != "pubsub" {
			t.Fatal(entry)
		}
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "redis://") {
		t.Fatal("inventory leaked connection configuration")
	}
	for range 20 {
		again, err := infrastructure.Configure(s, option)
		if err != nil {
			t.Fatal(err)
		}
		other, _ := json.Marshal(again.RedisKeys())
		if string(other) != string(encoded) {
			t.Fatal("inventory is nondeterministic")
		}
	}
	s.Cache.Stores["default"] = infrastructure.DefaultCacheSettings()
	inventory[0].Keys[0].Prefix = "changed"
	inventory[0].Channels[0].Prefix = "changed"
	other, _ := json.Marshal(plan.RedisKeys())
	if string(other) != string(encoded) {
		t.Fatal("inspection or caller settings mutated the plan")
	}
}

func TestRedisInventoryMemoryStoresAndUnusedConnectionsContributeNothing(t *testing.T) {
	s, _ := inventorySettings(keyspace.Namespace{Application: "inventory", Environment: "test"}, inventoryOfflineConnection())
	for name := range s.Jobs.Connections {
		s.Jobs.Connections[name] = infrastructure.DefaultJobConnectionSettings()
	}
	for name := range s.Cache.Stores {
		s.Cache.Stores[name] = infrastructure.DefaultCacheSettings()
	}
	s.Coordination.Driver = infrastructure.MemoryCoordination
	s.Realtime.Connections["default"] = infrastructure.DefaultRealtimeConnectionSettings()
	s.PubSub.Connections["default"] = infrastructure.DefaultBrokerSettings()
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(plan.RedisKeys())
	if string(wire) != "[]" {
		t.Fatalf("memory inventory: %s", wire)
	}
}

func TestRedisStoreDeclarationsValidateAndDeduplicate(t *testing.T) {
	s := infrastructure.DefaultSettings()
	s.Redis.Connections = infrastructure.RedisConnections{"default": inventoryOfflineConnection()}
	config := ratelimit.DefaultConfig(keyspace.Namespace{})
	first := infrastructure.RedisRateLimitStore(inventoryLimits, "", config)
	second := infrastructure.RedisRateLimitStore(inventoryExtraLimits, "default", config)
	plan, err := infrastructure.Configure(s, infrastructure.WithRedisStores(first, second))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.RedisKeys()[0].Keys) != 1 {
		t.Fatal("same namespace was not deduplicated")
	}
	invalid := config
	invalid.Namespace = keyspace.Namespace{Application: "wild*card", Environment: "production"}
	for name, declarations := range map[string][]infrastructure.RedisStore{
		"duplicate":          {first, first},
		"nil":                {nil},
		"unknown connection": {infrastructure.RedisRateLimitStore(inventoryLimits, "missing", config)},
		"invalid namespace":  {infrastructure.RedisRateLimitStore(inventoryLimits, "", invalid)},
		"zero key":           {infrastructure.RedisRateLimitStore(foundation.Key[*ratelimit.Store]{}, "", config)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := infrastructure.Configure(s, infrastructure.WithRedisStores(declarations...)); err == nil {
				t.Fatal("accepted invalid declaration")
			}
		})
	}
}
