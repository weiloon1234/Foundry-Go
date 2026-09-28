package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/leasetest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func leaseFixture(t *testing.T) (*Client, func(string) lease.Key) {
	t.Helper()
	c, namespace, track := integrationAddresses(t, nil)
	return c, func(text string) lease.Key {
		k, err := lease.NewKey(namespace, "acceptance", text)
		if err != nil {
			t.Fatal(err)
		}
		track(k.String())
		return k
	}
}
func TestRedisSharedLeaseContract(t *testing.T) {
	leasetest.Run(t, func(t *testing.T) leasetest.Fixture {
		c, key := leaseFixture(t)
		return leasetest.Fixture{Backend: c, Key: key, Expire: func(key lease.Key) {
			if err := c.raw.PExpire(t.Context(), key.String(), 0).Err(); err != nil {
				t.Fatal(err)
			}
		}}
	})
}
func TestRedisLeaseCorruptionPreservesData(t *testing.T) {
	for _, kind := range []string{"oversize", "short", "zero", "hash", "persistent"} {
		t.Run(kind, func(t *testing.T) {
			c, key := leaseFixture(t)
			k := key(kind)
			owner, _ := lease.NewOwner()
			if kind == "hash" {
				if err := c.raw.HSet(t.Context(), k.String(), "value", "private").Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				payload := strings.Repeat("a", lease.OwnerBytes)
				switch kind {
				case "oversize":
					payload = strings.Repeat("x", 1<<20)
				case "short":
					payload = "x"
				case "zero":
					payload = string(make([]byte, lease.OwnerBytes))
				}
				ttl := time.Minute
				if kind == "persistent" {
					ttl = 0
				}
				if err := c.raw.Set(t.Context(), k.String(), payload, ttl).Err(); err != nil {
					t.Fatal(err)
				}
			}
			before := c.raw.Dump(t.Context(), k.String()).Val()
			for _, op := range []func() (bool, error){func() (bool, error) { return c.LeaseAcquire(t.Context(), k, owner, time.Minute) }, func() (bool, error) { return c.LeaseRenew(t.Context(), k, owner, time.Minute) }, func() (bool, error) { return c.LeaseRelease(t.Context(), k, owner) }} {
				ok, err := op()
				if ok || !errors.Is(err, fault.Invalid) {
					t.Fatal(ok, err)
				}
				if after := c.raw.Dump(t.Context(), k.String()).Val(); before != after {
					t.Fatal("corruption was mutated")
				}
			}
		})
	}
}
func TestRedisTypedLeaseAcrossClients(t *testing.T) {
	first, namespace, track := integrationAddresses(t, nil)
	second, err := Open(t.Context(), integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close(context.Background()) })
	decl := lease.Define("member-refresh", keyspace.StringKeys[string]())
	key, err := lease.NewKey(namespace, decl.Name(), "member-1")
	if err != nil {
		t.Fatal(err)
	}
	track(key.String())
	config := lease.DefaultConfig(namespace)
	a, err := lease.NewManager(first, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	b, err := lease.NewManager(second, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close(context.Background()) })
	left, _ := decl.Bind(a)
	right, _ := decl.Bind(b)
	g, ok, err := left.TryAcquire(t.Context(), "member-1", time.Second)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, ok, err := right.TryAcquire(t.Context(), "member-1", time.Second); err != nil || ok {
		t.Fatal(ok, err)
	}
	if err := g.Renew(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ttl := first.raw.PTTL(t.Context(), key.String()).Val(); ttl <= 0 || ttl > time.Second {
		t.Fatal("invalid renewal TTL", ttl)
	}
	if err := g.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	ran, err := right.With(t.Context(), "member-1", time.Second, 0, func(ctx context.Context) error {
		if _, ok, err := left.TryAcquire(ctx, "member-1", time.Second); err != nil || ok {
			t.Fatalf("two authorities: %v %v", ok, err)
		}
		return nil
	})
	if !ran || err != nil {
		t.Fatal(ran, err)
	}
	if present := first.raw.Exists(t.Context(), key.String()).Val(); present != 0 {
		t.Fatal("scope cleanup left key")
	}
}
