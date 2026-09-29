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
	// A current payload over this reader's bound is a miss for every read path,
	// but it is never reclaimed: a process with a larger bound still reads it.
	if _, hit, err := c.GetTagged(t.Context(), snapshot); err != nil || hit {
		t.Fatal(hit, err)
	}
	if _, _, hit, err := c.ReadSnapshot(t.Context(), base, []cache.EntryKey{tag}, true); err != nil || hit {
		t.Fatal(hit, err)
	}
	if values, err := c.GetManyTagged(t.Context(), []cache.TaggedKey{snapshot}); err != nil || values[0].Found {
		t.Fatal(values, err)
	}
	if found, err := c.ExistsTagged(t.Context(), snapshot); err != nil || found {
		t.Fatal(found, err)
	}
	if exists := c.raw.Exists(t.Context(), snapshot.DataKey().String()).Val(); exists != 1 {
		t.Fatal("a read deleted a payload that is only over this reader's bound")
	}
	larger := c.config
	larger.MaxValueBytes = 128
	other, err := Open(t.Context(), larger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close(context.Background()) })
	if data, hit, err := other.GetTagged(t.Context(), snapshot); err != nil || !hit || len(data) != 65 {
		t.Fatal("larger-bound reader lost a valid payload", hit, err)
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
	// A malformed envelope is replaced as a whole by the next write.
	if err := c.PutTagged(t.Context(), fresh, []byte("overwrite"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if got, fields := c.raw.HGet(t.Context(), fresh.DataKey().String(), "value").Val(), c.raw.HLen(t.Context(), fresh.DataKey().String()).Val(); got != "overwrite" || fields != 2 {
		t.Fatal("malformed layout was not replaced", got, fields)
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
		for _, script := range []string{taggedCacheScript, tagVersionsScript} {
			if runsScript(args, script) {
				forceEval(args, h.prefix+script)
				break
			}
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
			// Inspect directly: the fixture keeps denying the command for later scripts.
			if got := c.raw.HGet(t.Context(), snapshot.DataKey().String(), "value").Val(); got != "before" {
				t.Fatal("permission failure changed data", got)
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
	var stores []*cache.Store
	bind := func(client *Client) (cache.Cache[string, string], cache.Tags[string]) {
		store, err := cache.NewStore(client, cache.DefaultConfig(namespace))
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, store)
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
	type remembered struct {
		value string
		err   error
	}
	result := make(chan remembered, 1)
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	t.Cleanup(func() { cancel(); <-completed })
	go func() {
		defer close(completed)
		value, err := one.Remember(ctx, "profile", cache.For(time.Minute), func(ctx context.Context) (string, error) {
			close(entered)
			select {
			case <-release:
				return "old", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})
		result <- remembered{value, err}
	}()
	select {
	case <-entered:
	case got := <-result:
		t.Fatal("loader did not start", got.err)
	}
	if err := tags.Invalidate(t.Context(), "member"); err != nil {
		t.Fatal(err)
	}
	if err := two.Put(t.Context(), "profile", "new", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	close(release)
	// The stale owner still returns its own loaded value, but the publication
	// that crossed the invalidation is rejected and counted, never stored.
	if got := <-result; got.err != nil || got.value != "old" {
		t.Fatal("stale owner failed its request", got.value, got.err)
	}
	if stats := stores[0].Stats(); stats.WriteFailures != 1 {
		t.Fatal("stale publication was not reported", stats)
	}
	if got, hit, err := one.Get(t.Context(), "profile"); err != nil || !hit || got != "new" {
		t.Fatal(got, hit, err)
	}
}

// commandCounter counts commands the client sends, including script calls.
type commandCounter struct{ count *int }

func (h commandCounter) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h commandCounter) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h commandCounter) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		*h.count++
		return next(ctx, cmd)
	}
}
func TestRedisSnapshotReadResolvesMetadataInOneRoundTrip(t *testing.T) {
	c, f := taggedFixture(t, nil)
	base, tag := f.Key("snapshot-read"), f.Key("snapshot-tag")
	tags := []cache.EntryKey{tag}
	// Warm the script cache so the counted read is a single EVALSHA.
	if _, _, _, err := c.ReadSnapshot(t.Context(), base, tags, true); err != nil {
		t.Fatal(err)
	}
	commands := 0
	c.raw.AddHook(commandCounter{&commands})
	key, _, hit, err := c.ReadSnapshot(t.Context(), base, tags, true)
	if err != nil || hit || commands != 1 {
		t.Fatal("snapshot read was not one round trip", hit, err, commands)
	}
	f.Track(key.DataKey())
	if ttl := c.raw.PTTL(t.Context(), tag.String()).Val(); ttl < tagMetadataRetention-time.Hour {
		t.Fatal("new metadata has no idle retention", ttl)
	}
	if err := c.PutTagged(t.Context(), key, []byte("value"), cache.For(2*tagMetadataRetention)); err != nil {
		t.Fatal(err)
	}
	if ttl := c.raw.PTTL(t.Context(), tag.String()).Val(); ttl < 2*tagMetadataRetention-time.Hour {
		t.Fatal("metadata can expire before a finite entry written under it", ttl)
	}
	again, data, hit, err := c.ReadSnapshot(t.Context(), base, tags, true)
	if err != nil || !hit || string(data) != "value" || again.FillKey() != key.FillKey() {
		t.Fatal(string(data), hit, err)
	}
	if _, data, hit, err := c.ReadSnapshot(t.Context(), base, tags, false); err != nil || !hit || data != nil {
		t.Fatal("existence read transferred a payload", hit, err)
	}
	// Metadata written without expiry by earlier releases receives one on use.
	if err := c.raw.Persist(t.Context(), tag.String()).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.ReadSnapshot(t.Context(), base, tags, false); err != nil {
		t.Fatal(err)
	}
	if ttl := c.raw.PTTL(t.Context(), tag.String()).Val(); ttl <= 0 {
		t.Fatal("persistent metadata was not given a retention", ttl)
	}
	if err := c.InvalidateTags(t.Context(), tags); err != nil {
		t.Fatal(err)
	}
	if ttl := c.raw.PTTL(t.Context(), tag.String()).Val(); ttl <= 0 {
		t.Fatal("rotated metadata lost its retention", ttl)
	}
	// The obsolete payload is a miss and is reclaimed by the same read.
	if _, _, hit, err := c.ReadSnapshot(t.Context(), base, tags, true); err != nil || hit {
		t.Fatal(hit, err)
	}
	if exists := c.raw.Exists(t.Context(), key.DataKey().String()).Val(); exists != 0 {
		t.Fatal("obsolete payload was not reclaimed")
	}
	if err := c.raw.Set(t.Context(), tag.String(), "corrupt", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.ReadSnapshot(t.Context(), base, tags, true); !errors.Is(err, fault.Invalid) {
		t.Fatal("corrupt metadata was replaced by a read", err)
	}
}

// Direct tagged writes resolve (or create) metadata and mutate in one script.
func TestRedisSnapshotWritesResolveMetadataInOneRoundTrip(t *testing.T) {
	c, f := taggedFixture(t, nil)
	base, tag := f.Key("snapshot-write"), f.Key("snapshot-write-tag")
	tags := []cache.EntryKey{tag}
	address, err := cache.TaggedDataKey(base, tags)
	if err != nil {
		t.Fatal(err)
	}
	f.Track(address)
	// Warm the script cache so each counted write is a single EVALSHA.
	if _, err := c.ForgetSnapshot(t.Context(), base, tags); err != nil {
		t.Fatal(err)
	}
	commands := 0
	c.raw.AddHook(commandCounter{&commands})
	if err := c.PutSnapshot(t.Context(), base, tags, []byte("value"), cache.For(time.Minute)); err != nil || commands != 1 {
		t.Fatal("snapshot write was not one round trip", err, commands)
	}
	key, data, hit, err := c.ReadSnapshot(t.Context(), base, tags, true)
	if err != nil || !hit || string(data) != "value" {
		t.Fatal(string(data), hit, err)
	}
	if added, err := c.AddSnapshot(t.Context(), base, tags, []byte("other"), cache.For(time.Minute)); err != nil || added {
		t.Fatal("add replaced a current entry", added, err)
	}
	if changed, err := c.ExpireSnapshot(t.Context(), base, tags, cache.Forever()); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if ttl := c.raw.PTTL(t.Context(), address.String()).Val(); ttl != -1 {
		t.Fatal("expiry was not removed", ttl)
	}
	// After invalidation the old payload is obsolete: Add fills the new snapshot
	// and Forget of an obsolete payload reports absence.
	if err := c.InvalidateTags(t.Context(), tags); err != nil {
		t.Fatal(err)
	}
	if removed, err := c.ForgetSnapshot(t.Context(), base, tags); err != nil || removed {
		t.Fatal("obsolete payload was counted", removed, err)
	}
	if added, err := c.AddSnapshot(t.Context(), base, tags, []byte("fresh"), cache.For(time.Minute)); err != nil || !added {
		t.Fatal(added, err)
	}
	fresh, data, hit, err := c.ReadSnapshot(t.Context(), base, tags, true)
	if err != nil || !hit || string(data) != "fresh" || fresh.FillKey() == key.FillKey() {
		t.Fatal("write did not use the rotated snapshot", string(data), hit, err)
	}
	if _, err := c.IncrementSnapshot(t.Context(), base, tags, 1, cache.For(time.Minute)); !errors.Is(err, fault.Invalid) {
		t.Fatal("non-integer payload was incremented", err)
	}
	if _, err := c.ForgetSnapshot(t.Context(), base, tags); err != nil {
		t.Fatal(err)
	}
	if value, err := c.IncrementSnapshot(t.Context(), base, tags, 5, cache.For(time.Minute)); err != nil || value != 5 {
		t.Fatal(value, err)
	}
	// Missing metadata receives a fresh version inside the write.
	if err := c.raw.Del(t.Context(), tag.String()).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.PutSnapshot(t.Context(), base, tags, []byte("recreated"), cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ttl := c.raw.PTTL(t.Context(), tag.String()).Val(); ttl < tagMetadataRetention-time.Hour {
		t.Fatal("created metadata has no idle retention", ttl)
	}
	if _, data, hit, err := c.ReadSnapshot(t.Context(), base, tags, true); err != nil || !hit || string(data) != "recreated" {
		t.Fatal(string(data), hit, err)
	}
	if err := c.raw.Set(t.Context(), tag.String(), "corrupt", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.PutSnapshot(t.Context(), base, tags, []byte("lost"), cache.For(time.Minute)); !errors.Is(err, fault.Invalid) {
		t.Fatal("corrupt metadata was replaced by a write", err)
	}
}

// A typed Put, Get and counter Increment each cost one Redis round trip,
// including the automatic namespace stamp.
func TestRedisTypedOperationsCostOneRoundTrip(t *testing.T) {
	c, f := taggedFixture(t, nil)
	namespace := f.Key("typed-round-trip").Namespace()
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	f.Track(scope)
	store, err := cache.NewStore(c, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	values, err := cache.Define("round-trip-values", cache.StringKeys[string](), cache.JSON[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	counters, err := cache.DefineCounter("round-trip-counters", cache.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []cache.Name{"round-trip-values", "round-trip-counters"} {
		base, err := cache.NewEntryKey(namespace, name, "key")
		if err != nil {
			t.Fatal(err)
		}
		address, err := cache.TaggedDataKey(base, []cache.EntryKey{scope})
		if err != nil {
			t.Fatal(err)
		}
		f.Track(address)
	}
	// Warm every script so counted operations are single EVALSHA commands.
	if err := values.Put(t.Context(), "key", "warm", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := values.Get(t.Context(), "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := counters.Increment(t.Context(), "key", 1, cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	commands := 0
	c.raw.AddHook(commandCounter{&commands})
	if err := values.Put(t.Context(), "key", "value", cache.For(time.Minute)); err != nil || commands != 1 {
		t.Fatal("typed Put was not one round trip", err, commands)
	}
	if value, hit, err := values.Get(t.Context(), "key"); err != nil || !hit || value != "value" || commands != 2 {
		t.Fatal("typed Get was not one round trip", value, hit, err, commands)
	}
	if value, err := counters.Increment(t.Context(), "key", 2, cache.For(time.Minute)); err != nil || value != 3 || commands != 3 {
		t.Fatal("typed Increment was not one round trip", value, err, commands)
	}
}
