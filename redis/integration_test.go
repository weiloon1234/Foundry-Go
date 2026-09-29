package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func integrationConfig(t *testing.T) Config {
	t.Helper()
	address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
	if address == "" {
		if os.Getenv("FOUNDRY_TEST_REDIS_REQUIRED") == "1" {
			t.Fatal("FOUNDRY_TEST_REDIS_ADDR is required")
		}
		t.Skip("set FOUNDRY_TEST_REDIS_ADDR for real Redis acceptance")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal("invalid Redis test address")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal("invalid Redis test port")
	}
	c := explicitConfig()
	c.Host = host
	c.Port = uint16(number)
	c.User = os.Getenv("FOUNDRY_TEST_REDIS_USER")
	c.Password = secret.New(os.Getenv("FOUNDRY_TEST_REDIS_PASSWORD"))
	return c
}
func integrationClient(t *testing.T, configure func(*Config)) (*Client, func(string) cache.EntryKey) {
	client, key, _ := integrationTracked(t, configure)
	return client, key
}
func integrationTracked(t *testing.T, configure func(*Config)) (*Client, func(string) cache.EntryKey, func(cache.EntryKey)) {
	client, namespace, trackAddress := integrationAddresses(t, configure)
	track := func(key cache.EntryKey) { trackAddress(key.String()) }
	return client, func(logical string) cache.EntryKey {
		key, err := cache.NewEntryKey(namespace, "acceptance", logical)
		if err != nil {
			t.Fatal(err)
		}
		track(key)
		return key
	}, track
}
func integrationAddresses(t *testing.T, configure func(*Config)) (*Client, cache.Namespace, func(string)) {
	t.Helper()
	c := integrationConfig(t)
	if configure != nil {
		configure(&c)
	}
	client, err := Open(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	rand.Read(random[:])
	namespace := cache.Namespace{Application: "foundry-go", Environment: "redis-test-" + hex.EncodeToString(random[:])}
	var keys []string
	var keyMu sync.Mutex
	track := func(key string) { keyMu.Lock(); defer keyMu.Unlock(); keys = append(keys, key) }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Exact owned addresses only; never enumerate or reset a shared Redis database.
		if len(keys) > 0 {
			if err := client.raw.Del(ctx, keys...).Err(); err != nil {
				t.Error("owned Redis key cleanup failed")
			}
		}
		if err := client.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return client, namespace, track
}
func TestRedisSharedCacheContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cachetest.Backend, func(string) cache.EntryKey) { return integrationClient(t, nil) })
}
func TestRedisExpiryAndMutationTTL(t *testing.T) {
	c, key := integrationClient(t, nil)
	k := key("ttl")
	if err := c.Put(t.Context(), k, []byte("5"), cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := c.raw.PTTL(t.Context(), k.String()).Val()
	if ok, err := c.Add(t.Context(), k, []byte("9"), cache.Forever()); err != nil || ok {
		t.Fatal(ok, err)
	}
	if got, err := c.Increment(t.Context(), k, 1, cache.Forever()); err != nil || got != 6 {
		t.Fatal(got, err)
	}
	after := c.raw.PTTL(t.Context(), k.String()).Val()
	if after <= 0 || after > before {
		t.Fatal("existing TTL changed", before, after)
	}
	if err := c.Put(t.Context(), k, []byte("7"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if got := c.raw.PTTL(t.Context(), k.String()).Val(); got != -1 {
		t.Fatal("Forever is not persistent", got)
	}
	if err := c.Put(t.Context(), k, []byte("tiny"), cache.For(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if got := c.raw.PTTL(t.Context(), k.String()).Val(); got != -2 && (got < 0 || got > time.Millisecond) {
		t.Fatal("submillisecond expiry became persistent", got)
	}
	// Force expiry only for this owned address, without wall-clock polling.
	if err := c.raw.PExpire(t.Context(), k.String(), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := c.Get(t.Context(), k); err != nil || found {
		t.Fatal(found, err)
	}
	if got, err := c.Increment(t.Context(), k, 2, cache.For(time.Minute)); err != nil || got != 2 {
		t.Fatal(got, err)
	}
	if got := c.raw.PTTL(t.Context(), k.String()).Val(); got <= 0 || got > time.Minute {
		t.Fatal(got)
	}
}
func TestRedisCorruptionAndBoundsPreserveStoredData(t *testing.T) {
	c, key := integrationClient(t, func(c *Config) { c.MaxValueBytes = 64 })
	k := key("oversized")
	private := strings.Repeat("private", 20)
	if err := c.raw.Set(t.Context(), k.String(), private, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	// Data written under a larger bound is a miss, never a failure.
	if _, hit, err := c.Get(t.Context(), k); err != nil || hit {
		t.Fatal(hit, err)
	}
	if err := c.Put(t.Context(), key("write-bound"), []byte(private), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got := c.raw.Get(t.Context(), k.String()).Val(); got != private {
		t.Fatal("oversized stored value altered")
	}
	wrong := key("wrong-type")
	if err := c.raw.LPush(t.Context(), wrong.String(), "private").Err(); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := c.Get(t.Context(), wrong); err != nil || hit {
		t.Fatal(hit, err)
	}
	if got := c.raw.LLen(t.Context(), wrong.String()).Val(); got != 1 {
		t.Fatal("wrong type changed by a read")
	}
	if removed, err := c.Forget(t.Context(), wrong); err != nil || removed {
		t.Fatal(removed, err)
	}
	if exists := c.raw.Exists(t.Context(), wrong.String()).Val(); exists != 0 {
		t.Fatal("unusable entry was not forgotten")
	}
	if err := c.Put(t.Context(), k, []byte("small"), cache.Forever()); err != nil {
		t.Fatal("over-bound entry could not be replaced", err)
	}
}
func TestRedisWrongCredentialsFailStartupAndClose(t *testing.T) {
	c := integrationConfig(t)
	c.User = "foundry-go-nonexistent-acceptance-user"
	c.Password = secret.New("not-a-real-password")
	client, err := Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	first := client.Start(t.Context())
	if first == nil || strings.Contains(first.Error(), c.Password.Reveal()) {
		t.Fatal("authentication did not fail safely")
	}
	if err := client.Start(t.Context()); err != first {
		t.Fatal("failed start retried", err)
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.Stats().Open != 0 {
		t.Fatal("failed start leaked connections")
	}
}
