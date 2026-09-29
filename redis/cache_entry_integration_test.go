package redis

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

func TestRedisEntryContract(t *testing.T) {
	cachetest.RunEntries(t, func(t *testing.T) cachetest.EntryFixture {
		client, key, track := integrationTracked(t, nil)
		return cachetest.EntryFixture{Backend: client, Key: key, Track: track, CorruptTagged: func(key cache.EntryKey) {
			if err := client.raw.Set(t.Context(), key.String(), "corrupt envelope", time.Minute).Err(); err != nil {
				t.Fatal(err)
			}
		}}
	})
}
func TestRedisExpiryChangesAndCounterRetention(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			client, key, track := integrationTracked(t, nil)
			base := key("expiry")
			snapshot := (cachetest.TaggedFixture{Backend: client, Track: track}).Snapshot(t, base, key("tag"))
			physical := base.String()
			if tagged {
				physical = snapshot.DataKey().String()
			}
			put := func() error {
				if tagged {
					return client.PutTagged(t.Context(), snapshot, []byte("41"), cache.For(time.Second))
				}
				return client.Put(t.Context(), base, []byte("41"), cache.For(time.Second))
			}
			expire := func(ttl cache.TTL) (bool, error) {
				if tagged {
					return client.ExpireTagged(t.Context(), snapshot, ttl)
				}
				return client.Expire(t.Context(), base, ttl)
			}
			if err := put(); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if changed, err := expire(cache.Forever()); err != nil || !changed {
					t.Fatal(changed, err)
				}
			}
			if ttl := client.raw.PTTL(t.Context(), physical).Val(); ttl != -1 {
				t.Fatal("persistent expiry not applied", ttl)
			}
			if changed, err := expire(cache.For(time.Minute)); err != nil || !changed {
				t.Fatal(changed, err)
			}
			before := client.raw.PTTL(t.Context(), physical).Val()
			if before <= 0 || before > time.Minute {
				t.Fatal(before)
			}
			var value int64
			var err error
			if tagged {
				value, err = client.IncrementTagged(t.Context(), snapshot, 1, cache.Forever())
			} else {
				value, err = client.Increment(t.Context(), base, 1, cache.Forever())
			}
			if err != nil || value != 42 {
				t.Fatal(value, err)
			}
			if after := client.raw.PTTL(t.Context(), physical).Val(); after <= 0 || after > before {
				t.Fatal("counter replaced updated expiry", before, after)
			}
			if changed, err := expire(cache.For(time.Nanosecond)); err != nil || !changed {
				t.Fatal(changed, err)
			}
			if ttl := client.raw.PTTL(t.Context(), physical).Val(); ttl != -2 && (ttl < 0 || ttl > time.Millisecond) {
				t.Fatal("tiny expiry became persistent", ttl)
			}
		})
	}
}
func TestRedisBatchRemovesUnusablePlainEntriesWithoutCountingThem(t *testing.T) {
	c, key := integrationClient(t, nil)
	keys := []cache.EntryKey{key("one"), key("two")}
	if keys[0].String() > keys[1].String() {
		keys[0], keys[1] = keys[1], keys[0]
	}
	if err := c.Put(t.Context(), keys[0], []byte("live"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.LPush(t.Context(), keys[1].String(), "wrong type").Err(); err != nil {
		t.Fatal(err)
	}
	if count, err := c.ForgetMany(t.Context(), keys); count != 1 || err != nil {
		t.Fatal(count, err)
	}
	if exists := c.raw.Exists(t.Context(), keys[0].String(), keys[1].String()).Val(); exists != 0 {
		t.Fatal("batch left entries", exists)
	}
	if err := c.raw.LPush(t.Context(), keys[1].String(), "wrong type").Err(); err != nil {
		t.Fatal(err)
	}
	if found, err := c.Exists(t.Context(), keys[1]); err != nil || found {
		t.Fatal(found, err)
	}
	if changed, err := c.Expire(t.Context(), keys[1], cache.Forever()); err != nil || changed {
		t.Fatal(changed, err)
	}
	if err := c.Put(t.Context(), keys[1], []byte("replacement"), cache.Forever()); err != nil {
		t.Fatal("wrong-type entry could not be replaced", err)
	}
	if data, hit, err := c.Get(t.Context(), keys[1]); err != nil || !hit || string(data) != "replacement" {
		t.Fatal(string(data), hit, err)
	}
}
func TestRedisBatchReadsPlainAndTaggedEntries(t *testing.T) {
	c, key, track := integrationTracked(t, nil)
	keys := []cache.EntryKey{key("read-a"), key("read-b"), key("read-c")}
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	if err := c.Put(t.Context(), keys[0], []byte("zero"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.LPush(t.Context(), keys[2].String(), "wrong type").Err(); err != nil {
		t.Fatal(err)
	}
	values, err := c.GetMany(t.Context(), keys)
	if err != nil || len(values) != 3 || !values[0].Found || string(values[0].Data) != "zero" || values[1].Found || values[2].Found {
		t.Fatal(values, err)
	}
	if _, err := c.GetMany(t.Context(), []cache.EntryKey{keys[1], keys[0]}); !errors.Is(err, fault.Invalid) {
		t.Fatal("non-canonical batch accepted", err)
	}

	fixture := cachetest.TaggedFixture{Backend: c, Track: track}
	first := fixture.Snapshot(t, key("tagged-a"), key("batch-tag"))
	second, err := cache.NewTaggedKey(key("tagged-b"), first.Stamps())
	if err != nil {
		t.Fatal(err)
	}
	batch := []cache.TaggedKey{first, second}
	slices.SortFunc(batch, func(a, b cache.TaggedKey) int { return strings.Compare(a.DataKey().String(), b.DataKey().String()) })
	for _, k := range batch {
		track(k.DataKey())
	}
	if err := c.PutTagged(t.Context(), first, []byte("tagged"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	read, err := c.GetManyTagged(t.Context(), batch)
	if err != nil || len(read) != 2 {
		t.Fatal(read, err)
	}
	for i, k := range batch {
		want := k.DataKey().String() == first.DataKey().String()
		if read[i].Found != want || want && string(read[i].Data) != "tagged" {
			t.Fatal("tagged batch result mismatch", i, read[i])
		}
	}
	if err := c.InvalidateTags(t.Context(), []cache.EntryKey{key("batch-tag")}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetManyTagged(t.Context(), batch); !errors.Is(err, fault.Conflict) {
		t.Fatal("stale snapshot batch read did not conflict", err)
	}
}
