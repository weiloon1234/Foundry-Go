package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

func taggedFixture(t *testing.T, configure func(*Config)) (*Client, cachetest.TaggedFixture) {
	t.Helper()
	c, key, track := integrationTracked(t, configure)
	return c, cachetest.TaggedFixture{Backend: c, Key: key, Track: track}
}
func TestRedisTaggedExpiryAndPersistentAddress(t *testing.T) {
	c, f := taggedFixture(t, nil)
	base := f.Key("value")
	tag := f.Key("tag")
	snapshot := f.Snapshot(t, base, tag)
	if err := c.PutTagged(t.Context(), snapshot, []byte("5"), cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val()
	if ok, err := c.AddTagged(t.Context(), snapshot, []byte("6"), cache.Forever()); err != nil || ok {
		t.Fatal(ok, err)
	}
	if got, err := c.IncrementTagged(t.Context(), snapshot, 1, cache.Forever()); err != nil || got != 6 {
		t.Fatal(got, err)
	}
	after := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val()
	if after <= 0 || after > before {
		t.Fatal("existing tagged TTL changed", before, after)
	}
	if err := c.PutTagged(t.Context(), snapshot, []byte("7"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if expiry := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val(); expiry != -1 {
		t.Fatal(expiry)
	}
	for range 25 {
		if err := c.InvalidateTags(t.Context(), []cache.EntryKey{tag}); err != nil {
			t.Fatal(err)
		}
		fresh := f.Snapshot(t, base, tag)
		if fresh.DataKey() != snapshot.DataKey() {
			t.Fatal("invalidation allocated a generation address")
		}
		if ok, err := c.AddTagged(t.Context(), fresh, []byte("value"), cache.Forever()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if size := c.raw.HLen(t.Context(), fresh.DataKey().String()).Val(); size != 2 {
			t.Fatal("generation fields accumulated", size)
		}
		snapshot = fresh
	}
	if err := c.PutTagged(t.Context(), snapshot, []byte("tiny"), cache.For(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if expiry := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val(); expiry != -2 && (expiry < 0 || expiry > time.Millisecond) {
		t.Fatal(expiry)
	}
	if err := c.raw.PExpire(t.Context(), snapshot.DataKey().String(), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := c.GetTagged(t.Context(), snapshot); err != nil || hit {
		t.Fatal(hit, err)
	}
}
func TestRedisTagMetadataEnvelopeAndPayloadBounds(t *testing.T) {
	c, f := taggedFixture(t, func(c *Config) { c.MaxValueBytes = 64 })
	tag := f.Key("metadata")
	base := f.Key("payload")
	for _, invalid := range []string{strings.Repeat("x", cache.TagVersionBytes), tagMetadataPrefix + string(make([]byte, cache.TagVersionBytes)), strings.Repeat("large-private", 1024)} {
		if err := c.raw.Set(t.Context(), tag.String(), invalid, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ResolveTags(t.Context(), []cache.EntryKey{tag}); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if got := c.raw.Get(t.Context(), tag.String()).Val(); got != invalid {
			t.Fatal("corrupt metadata replaced")
		}
	}
	if err := c.raw.Del(t.Context(), tag.String()).Err(); err != nil {
		t.Fatal(err)
	}
	snapshot := f.Snapshot(t, base, tag)
	if err := c.PutTagged(t.Context(), snapshot, []byte("value"), cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.HSet(t.Context(), snapshot.DataKey().String(), "value", strings.Repeat("x", 65)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.GetTagged(t.Context(), snapshot); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if size := c.raw.HStrLen(t.Context(), snapshot.DataKey().String(), "value").Val(); size != 65 {
		t.Fatal("oversize read altered data")
	}
	if err := c.InvalidateTags(t.Context(), []cache.EntryKey{tag}); err != nil {
		t.Fatal(err)
	}
	fresh := f.Snapshot(t, base, tag)
	if _, hit, err := c.GetTagged(t.Context(), fresh); err != nil || hit {
		t.Fatal("obsolete oversize payload was read", hit, err)
	}
	if err := c.PutTagged(t.Context(), fresh, []byte("new"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.HSet(t.Context(), fresh.DataKey().String(), "unexpected", "x").Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.PutTagged(t.Context(), fresh, []byte("overwrite"), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got := c.raw.HGet(t.Context(), fresh.DataKey().String(), "value").Val(); got != "new" {
		t.Fatal("corrupt layout changed")
	}
}

// This fixture shadows only the script-local permission function. Redis globals,
// ACL users and shared server configuration are untouched; real storage commands
// still execute through the production script and adapter path.
type scriptPermissionFixture struct{ prefix string }

func (h scriptPermissionFixture) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h scriptPermissionFixture) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h scriptPermissionFixture) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		if len(args) > 1 && args[0] == "eval" && (args[1] == taggedCacheScript || args[1] == tagVersionsScript) {
			args[1] = h.prefix + args[1].(string)
		}
		return next(ctx, cmd)
	}
}
func TestRedisTaggedPermissionsFailBeforeMutation(t *testing.T) {
	for _, denied := range []string{"HSET", "PEXPIRE", "PERSIST"} {
		t.Run(denied, func(t *testing.T) {
			c, f := taggedFixture(t, nil)
			snapshot := f.Snapshot(t, f.Key("value"), f.Key("tag"))
			if err := c.PutTagged(t.Context(), snapshot, []byte("before"), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			before := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val()
			prefix := fmt.Sprintf("local real_redis=redis\nlocal redis=setmetatable({acl_check_cmd=function(command,...) if command=='%s' then return false end return real_redis.acl_check_cmd(command,...) end},{__index=real_redis})\n", denied)
			c.raw.AddHook(scriptPermissionFixture{prefix: prefix})
			ttl := cache.For(time.Minute)
			if denied == "PERSIST" {
				ttl = cache.Forever()
			}
			if err := c.PutTagged(t.Context(), snapshot, []byte("after"), ttl); !errors.Is(err, fault.Internal) {
				t.Fatal(err)
			}
			if got, hit, err := c.GetTagged(t.Context(), snapshot); err != nil || !hit || string(got) != "before" {
				t.Fatal("permission failure changed data", hit, err)
			}
			if after := c.raw.PTTL(t.Context(), snapshot.DataKey().String()).Val(); after <= 0 || after > before {
				t.Fatal("permission failure changed TTL", before, after)
			}
		})
	}
}
func TestRedisMissingTagCapabilityFailsBeforeMetadataWrite(t *testing.T) {
	c, f := taggedFixture(t, nil)
	key := f.Key("tag")
	c.raw.AddHook(scriptPermissionFixture{prefix: "local real_redis=redis\nlocal redis=setmetatable({acl_check_cmd=false},{__index=real_redis})\n"})
	if _, err := c.ResolveTags(t.Context(), []cache.EntryKey{key}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if exists := c.raw.Exists(t.Context(), key.String()).Val(); exists != 0 {
		t.Fatal("unsupported feature wrote metadata")
	}
	// The ordinary cache capability does not depend on the tagged script feature.
	if err := c.Put(t.Context(), key, []byte("ordinary"), cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
}
func TestRedisCrossClientInvalidationRejectsRememberOwner(t *testing.T) {
	first, key, track := integrationTracked(t, nil)
	second, err := Open(t.Context(), first.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	namespace := key("namespace-owner").Namespace()
	values := cache.Define("typed-values", cache.StringKeys[string](), cache.JSON[string]())
	changes := cache.DefineTag("typed-changes", cache.StringKeys[string]())
	bind := func(client *Client) (cache.Cache[string, string], cache.Tags[string]) {
		store, err := cache.NewStore(client, cache.DefaultConfig(namespace))
		if err != nil {
			t.Fatal(err)
		}
		profiles, err := values.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		tags, err := changes.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		profiles, err = profiles.WithTags(tags.For("member"))
		if err != nil {
			t.Fatal(err)
		}
		return profiles, tags
	}
	one, _ := bind(first)
	two, tags := bind(second)
	base, err := cache.NewEntryKey(namespace, values.Name(), "profile")
	if err != nil {
		t.Fatal(err)
	}
	tag, err := cache.NewEntryKey(namespace, changes.Name(), "member")
	if err != nil {
		t.Fatal(err)
	}
	track(tag)
	fixture := cachetest.TaggedFixture{Backend: first, Track: track}
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	track(scope)
	fixture.Snapshot(t, base, tag, scope)
	entered := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	t.Cleanup(func() { cancel(); <-completed })
	go func() {
		defer close(completed)
		_, err := one.Remember(ctx, "profile", cache.For(time.Minute), func(ctx context.Context) (string, error) {
			close(entered)
			select {
			case <-release:
				return "old", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})
		result <- err
	}()
	select {
	case <-entered:
	case err := <-result:
		t.Fatal("loader did not start", err)
	}
	if err := tags.Invalidate(t.Context(), "member"); err != nil {
		t.Fatal(err)
	}
	if err := two.Put(t.Context(), "profile", "new", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, fault.Conflict) {
		t.Fatal("stale loader published", err)
	}
	if got, hit, err := one.Get(t.Context(), "profile"); err != nil || !hit || got != "new" {
		t.Fatal(got, hit, err)
	}
}
