package memory_test

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/leasetest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

func TestSharedContract(t *testing.T) {
	leasetest.Run(t, func(t *testing.T) leasetest.Fixture {
		b, err := memory.New(32)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		return leasetest.Fixture{Backend: b, Key: func(text string) lease.Key {
			k, err := lease.NewKey(keyspace.Namespace{Application: "test", Environment: "memory"}, "leases", text)
			if err != nil {
				t.Fatal(err)
			}
			return k
		}, Expire: func(lease.Key) { time.Sleep(lease.MinDuration) }}
	})
}
func TestCapacityNeverEvictsLiveOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, _ := memory.New(1)
		a, _ := lease.NewKey(keyspace.Namespace{Application: "test", Environment: "memory"}, "leases", "a")
		z, _ := lease.NewKey(a.Namespace(), "leases", "z")
		owner, _ := lease.NewOwner()
		if ok, err := b.LeaseAcquire(t.Context(), a, owner, lease.MinDuration); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if ok, err := b.LeaseAcquire(t.Context(), z, owner, time.Minute); ok || !errors.Is(err, fault.Conflict) {
			t.Fatal(ok, err)
		}
		if ok, err := b.LeaseRenew(t.Context(), a, owner, lease.MinDuration); err != nil || !ok {
			t.Fatal("live owner evicted", ok, err)
		}
		time.Sleep(lease.MinDuration)
		if ok, err := b.LeaseAcquire(t.Context(), z, owner, time.Minute); err != nil || !ok {
			t.Fatal(ok, err)
		}
		b.Close()
		if ok, err := b.LeaseRenew(t.Context(), z, owner, time.Minute); ok || !errors.Is(err, fault.Closed) {
			t.Fatal(ok, err)
		}
	})
}
func TestRenewalReordersExpiryAndForceReleaseRepairs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, _ := memory.New(2)
		defer b.Close()
		a, _ := lease.NewKey(keyspace.Namespace{Application: "test", Environment: "memory"}, "leases", "a")
		z, _ := lease.NewKey(a.Namespace(), "leases", "z")
		c, _ := lease.NewKey(a.Namespace(), "leases", "c")
		owner, _ := lease.NewOwner()
		for _, key := range []lease.Key{a, z} {
			if ok, err := b.LeaseAcquire(t.Context(), key, owner, lease.MinDuration); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		// A renewed owner moves behind the other deadline and must survive reclamation.
		if ok, err := b.LeaseRenew(t.Context(), a, owner, time.Minute); err != nil || !ok {
			t.Fatal(ok, err)
		}
		time.Sleep(lease.MinDuration)
		if ok, err := b.LeaseAcquire(t.Context(), c, owner, time.Minute); err != nil || !ok {
			t.Fatal("expired owner was not reclaimed", ok, err)
		}
		if ok, err := b.LeaseRenew(t.Context(), a, owner, time.Minute); err != nil || !ok {
			t.Fatal("renewed owner was reclaimed", ok, err)
		}
		if removed, err := b.LeaseForceRelease(t.Context(), a); err != nil || !removed {
			t.Fatal(removed, err)
		}
		if ok, err := b.LeaseRenew(t.Context(), a, owner, time.Minute); err != nil || ok {
			t.Fatal("forced release kept the owner", ok, err)
		}
	})
}
