package redis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type cleanupTest struct {
	testing.TB
	failed   bool
	cleanups []func()
	logs     []string
	errors   []string
}

func (t *cleanupTest) Failed() bool      { return t.failed }
func (t *cleanupTest) Cleanup(fn func()) { t.cleanups = append(t.cleanups, fn) }
func (t *cleanupTest) Logf(format string, args ...any) {
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
}
func (t *cleanupTest) Errorf(format string, args ...any) {
	t.errors = append(t.errors, fmt.Sprintf(format, args...))
	t.failed = true
}
func (t *cleanupTest) run() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
}

func TestCleanupRetentionWithoutRedis(t *testing.T) {
	namespace := keyspace.Namespace{Application: "owned-test", Environment: "test"}
	prefix := keyaddress.Prefix(cache.KeyFeature, namespace)
	for _, helper := range []string{"inventory", "namespace"} {
		for _, reason := range []string{"failure", "keep variable", "shutdown failure"} {
			t.Run(helper+"/"+reason, func(t *testing.T) {
				// No Redis is running here: retained cleanup must not attempt a connection.
				t.Setenv(AddressVariable, "127.0.0.1:1")
				t.Setenv(KeepKeysVariable, "0")
				probe := &cleanupTest{TB: t, failed: reason == "failure"}
				if reason == "keep variable" {
					t.Setenv(KeepKeysVariable, "1")
				}
				if helper == "inventory" {
					inventory := []infrastructure.RedisConnectionInventory{{Keys: []infrastructure.RedisKeySpace{{Feature: cache.KeyFeature, Namespace: namespace, Version: keyaddress.Version, Prefix: prefix}}}}
					Cleanup(probe, inventory)
					// Later caller mutations must not change what is retained or deleted.
					inventory[0].Keys[0].Prefix = "changed"
				} else {
					CleanupNamespace(probe, namespace, cache.KeyFeature)
				}
				if len(probe.cleanups) != 1 {
					t.Fatal("cleanup was not registered at call time")
				}
				if reason == "shutdown failure" {
					probe.Cleanup(func() { probe.failed = true })
				}
				probe.run()
				if len(probe.errors) != 0 {
					t.Fatalf("retained cleanup attempted Redis I/O: %v", probe.errors)
				}
				if len(probe.logs) != 1 || !strings.Contains(probe.logs[0], prefix) {
					t.Fatalf("retained exact prefix was not logged: %v", probe.logs)
				}
			})
		}
	}
}

func TestCleanupIgnoresChannelsWithoutRedis(t *testing.T) {
	t.Setenv(AddressVariable, "")
	probe := &cleanupTest{TB: t}
	Cleanup(probe, []infrastructure.RedisConnectionInventory{{Channels: []infrastructure.RedisKeySpace{{Prefix: "channel-only"}}}})
	probe.run()
	if len(probe.errors) != 0 || len(probe.logs) != 0 {
		t.Fatal("channels became key cleanup work")
	}
}
