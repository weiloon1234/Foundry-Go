package redis

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/scheduletest"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func TestRedisSchedulerLeadershipAndOwnedFailover(t *testing.T) {
	client, namespace, track := integrationAddresses(t, nil)
	scheduletest.Run(t, client, namespace, track)
}

func TestRedisScheduleCursorOnlyAdvances(t *testing.T) {
	client, namespace, track := integrationAddresses(t, nil)
	key, err := lease.NewKey(namespace, "foundry.schedule.cursor", "default:reports")
	if err != nil {
		t.Fatal(err)
	}
	track(scheduleCursorKey(key))
	if _, found, err := client.ScheduleCursor(t.Context(), key); err != nil || found {
		t.Fatal("missing cursor was reported", found, err)
	}
	later := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{later, later.Add(-time.Hour)} {
		if err := client.AdvanceScheduleCursor(t.Context(), key, at, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	at, found, err := client.ScheduleCursor(t.Context(), key)
	if err != nil || !found || !at.Equal(later) {
		t.Fatal("cursor moved backwards or was lost", at, found, err)
	}
	if ttl := client.raw.PTTL(t.Context(), scheduleCursorKey(key)).Val(); ttl <= 0 || ttl > time.Hour {
		t.Fatal("cursor expiry was not refreshed", ttl)
	}
	if err := client.AdvanceScheduleCursor(t.Context(), key, later.Add(time.Nanosecond), time.Hour); !errors.Is(err, fault.Invalid) {
		t.Fatal("sub-millisecond cursor accepted", err)
	}
}
