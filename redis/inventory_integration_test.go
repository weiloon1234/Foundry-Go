package redis_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func inventoryOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type inventoryJob struct{ Value string }

func TestRedisInventoryRuntimePrefixesAndACL(t *testing.T) {
	config := redistest.Config(t)
	namespace := keyspace.Namespace{Application: "b28-" + rand.Text(), Environment: "test-" + rand.Text()}
	settings, declarations := inventorySettings(namespace, infrastructure.RedisSettingsFromConfig(config))
	plan, err := infrastructure.Configure(settings, infrastructure.WithRedisStores(declarations...))
	inventoryOK(t, err)
	inventory := plan.RedisKeys()
	admin := driver.NewClient(&driver.Options{Addr: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), DB: config.Database, MaxRetries: -1})
	t.Cleanup(func() { inventoryOK(t, admin.Close()) })
	info, err := admin.Info(t.Context(), "server").Result()
	inventoryOK(t, err)
	var major int
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			_, err = fmt.Sscanf(line, "redis_version:%d.", &major)
			inventoryOK(t, err)
		}
	}
	if major < 7 {
		t.Fatal("inventory ACL acceptance requires Redis 7 or newer")
	}

	// Scan an independent list of exact test namespaces, with any feature. Scanning
	// only inventory prefixes would hide a missing feature from this assertion.
	suffixes := []string{"", ".default", ".override", ".jobs.default", ".jobs.mail", ".coordination", ".limits", ".realtime.default", ".pubsub.default"}
	scan := func(ctx context.Context) ([]string, error) {
		var all []string
		for _, suffix := range suffixes {
			pattern := "foundry:*:v1:" + namespace.Application + suffix + ":" + namespace.Environment + ":*"
			var cursor uint64
			for {
				batch, next, err := admin.Scan(ctx, cursor, pattern, 100).Result()
				if err != nil {
					return nil, err
				}
				all = append(all, batch...)
				cursor = next
				if cursor == 0 {
					break
				}
			}
		}
		return all, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		keys, err := scan(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		for _, key := range keys {
			if err := admin.Del(ctx, key).Err(); err != nil {
				t.Error(err)
			}
		}
	})
	observed := make(map[string]bool)
	observe := func(ctx context.Context) error {
		keys, err := scan(ctx)
		if err != nil {
			return err
		}
		for _, key := range keys {
			observed[key] = true
		}
		return nil
	}

	definition := jobs.Define[inventoryJob]("inventory", 1, jobs.DefaultPolicy("default"))
	jobsModule := foundation.Module{Name: "inventory.jobs", OnRegister: func(r *foundation.Registrar) error {
		for _, name := range []jobs.ConnectionName{"default", "mail"} {
			if err := jobs.RegisterJob(r, infrastructure.JobDispatcherKey(name), definition, func(foundation.Resolver) (jobs.Declaration, error) {
				return definition.Declare(func(context.Context, inventoryJob) error { return nil })
			}); err != nil {
				return err
			}
		}
		return nil
	}}
	app, err := plan.Register(foundation.NewBuilder()).Register(jobsModule).Build(t.Context())
	inventoryOK(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		inventoryOK(t, app.Shutdown(ctx))
	})
	inventoryOK(t, app.Start(t.Context()))
	services, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	inventoryOK(t, err)
	for _, name := range []jobs.ConnectionName{"default", "mail"} {
		connection, err := services.Jobs.Connection(name)
		inventoryOK(t, err)
		bound, err := definition.On(connection)
		inventoryOK(t, err)
		_, err = bound.Dispatch(t.Context(), inventoryJob{Value: "inventory"}, jobs.Options[inventoryJob]{})
		inventoryOK(t, err)
	}
	manager, err := services.Leases()
	inventoryOK(t, err)
	leases, err := lease.Define("inventory", keyspace.StringKeys[string]()).Bind(manager)
	inventoryOK(t, err)
	guard, acquired, err := leases.TryAcquire(t.Context(), "key", time.Minute)
	inventoryOK(t, err)
	if !acquired {
		t.Fatal("test lease was not acquired")
	}
	defer guard.Release(context.Background())
	for _, name := range []cache.StoreName{"default", "second"} {
		store, err := services.Caches.Store(name)
		inventoryOK(t, err)
		entries, err := cache.Define("inventory", keyspace.StringKeys[string](), cache.JSON[string]()).Bind(store)
		inventoryOK(t, err)
		tags, err := cache.DefineTag("inventory-tags", keyspace.StringKeys[string]()).Bind(store)
		inventoryOK(t, err)
		tagged, err := entries.WithTags(tags.For("tag"))
		inventoryOK(t, err)
		_, err = tagged.Remember(t.Context(), "key", cache.Forever(), func(ctx context.Context) (string, error) {
			// Observe the actual fill lease while the loader owns it.
			return "value", observe(ctx)
		})
		inventoryOK(t, err)
		inventoryOK(t, store.Invalidate(t.Context()))
	}
	for _, key := range []foundation.Key[*ratelimit.Store]{inventoryLimits, inventoryExtraLimits} {
		store, err := foundation.Resolve(app.Services(), key)
		inventoryOK(t, err)
		limiter, err := ratelimit.Define("inventory", keyspace.StringKeys[string](), ratelimit.Limit{Requests: 10, Window: time.Minute}).Bind(store)
		inventoryOK(t, err)
		_, err = limiter.Allow(t.Context(), "key")
		inventoryOK(t, err)
	}
	protection, err := foundation.Resolve(app.Services(), inventoryLockout)
	inventoryOK(t, err)
	throttle, err := lockout.Define("inventory", keyspace.StringKeys[string](), lockout.DefaultPolicy()).Bind(protection)
	inventoryOK(t, err)
	_, err = throttle.Run(t.Context(), "key", func(context.Context) (bool, error) { return false, nil })
	inventoryOK(t, err)

	broker, err := services.Broker()
	inventoryOK(t, err)
	topic, err := pubsub.Define[string, string]("inventory", 1, keyspace.StringKeys[string]()).Bind(broker)
	inventoryOK(t, err)
	subscription, err := topic.Subscribe(t.Context(), "key")
	inventoryOK(t, err)
	defer subscription.Close(context.Background())
	_, err = topic.Publish(t.Context(), "key", "value")
	inventoryOK(t, err)
	_, err = subscription.Receive(t.Context())
	inventoryOK(t, err)
	channel := websocket.Public[struct{}]("inventory", websocket.DefineRooms(foundryhttp.StringPath[string]())).WithReplay(websocket.ReplayConfig{Messages: 2, Bytes: 8192, TTL: time.Minute})
	event := websocket.RawOutgoing(channel, "message")
	registry, err := websocket.NewRegistry(websocket.Register(channel, event.Registration()))
	inventoryOK(t, err)
	realtime, err := services.RealtimeConnection()
	inventoryOK(t, err)
	publisher, err := realtime.NewPublisher(registry, websocket.DefaultConfig())
	inventoryOK(t, err)
	defer publisher.Close(context.Background())
	_, err = websocket.Publish(t.Context(), publisher, channel, "room", event, json.RawMessage(`{"inventory":true}`))
	inventoryOK(t, err)
	inventoryOK(t, observe(t.Context()))

	seenPrefixes := make(map[string]bool)
	for key := range observed {
		matched := false
		for _, connection := range inventory {
			for _, space := range connection.Keys {
				if strings.HasPrefix(key, space.Prefix) {
					matched = true
					seenPrefixes[space.Prefix] = true
				}
			}
		}
		if !matched {
			t.Errorf("runtime wrote a key outside the inventory: %s", key)
		}
	}
	for _, connection := range inventory {
		for _, space := range connection.Keys {
			if !seenPrefixes[space.Prefix] {
				t.Errorf("adapter prefix was not exercised: %s", space.Prefix)
			}
		}
	}

	// One fresh ACL identity per named connection proves that grants do not leak
	// across connection groups. Existing users are never modified.
	for _, connection := range inventory {
		t.Run(string(connection.Connection), func(t *testing.T) {
			username, password := "foundry-b28-"+rand.Text(), rand.Text()
			prior, err := admin.Do(t.Context(), "ACL", "GETUSER", username).Result()
			if err != nil && !errors.Is(err, driver.Nil) {
				inventoryOK(t, err)
			}
			if prior != nil {
				t.Fatal("random ACL identity already exists")
			}
			args := []string{"reset", "on", ">" + password, "resetkeys", "resetchannels", "+get", "+set", "+ping", "+publish"}
			for _, space := range connection.Keys {
				args = append(args, "~"+space.Prefix+"*")
			}
			// Cleanup is registered before the command in case its outcome is uncertain.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				inventoryOK(t, admin.ACLDelUser(ctx, username).Err())
			})
			inventoryOK(t, admin.ACLSetUser(t.Context(), username, args...).Err())
			scoped := driver.NewClient(&driver.Options{Addr: admin.Options().Addr, DB: config.Database, Username: username, Password: password, MaxRetries: -1, Protocol: 2})
			defer scoped.Close()
			inventoryOK(t, scoped.Ping(t.Context()).Err())
			for _, space := range connection.Keys {
				key := space.Prefix + "acl-probe"
				inventoryOK(t, scoped.Set(t.Context(), key, "allowed", time.Minute).Err())
				inventoryOK(t, scoped.Get(t.Context(), key).Err())
				sibling := "foundry:" + string(space.Feature) + ":v1:" + space.Namespace.Application + ".sibling:" + space.Namespace.Environment + ":acl-probe"
				crafted := "foundry:" + string(space.Feature) + ":v1:" + space.Namespace.Application + ".evil:staging:payload:" + space.Namespace.Environment + ":acl-probe"
				for _, denied := range []string{sibling, crafted} {
					if err := scoped.Get(t.Context(), denied).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
						t.Fatalf("foreign key was not denied: %v", err)
					}
					if err := scoped.Set(t.Context(), denied, "denied", time.Minute).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
						// Clean up even if a regression allowed the deliberately forbidden write.
						inventoryOK(t, admin.Del(t.Context(), denied).Err())
						t.Fatalf("foreign write was not denied: %v", err)
					}
				}
			}
			if err := scoped.Publish(t.Context(), "foundry:pubsub:v1:"+namespace.Application+":"+namespace.Environment+":probe", "denied").Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
				t.Fatalf("channels were not denied: %v", err)
			}
			for _, other := range inventory {
				if other.Connection == connection.Connection {
					continue
				}
				for _, space := range other.Keys {
					if err := scoped.Get(t.Context(), space.Prefix+"acl-probe").Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
						t.Fatalf("another connection's namespace was not denied: %v", err)
					}
				}
			}
		})
	}
}
