package redis

import (
	"errors"
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
func TestRedisBatchLaterPlainCorruptionDoesNotDeleteEarlierKeys(t *testing.T) {
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
	if count, err := c.ForgetMany(t.Context(), keys); count != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal(count, err)
	}
	if data, hit, err := c.Get(t.Context(), keys[0]); err != nil || !hit || string(data) != "live" {
		t.Fatal("earlier key deleted", string(data), hit, err)
	}
	if _, err := c.Exists(t.Context(), keys[1]); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.Expire(t.Context(), keys[1], cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
