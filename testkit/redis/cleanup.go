package redis

import (
	"net"
	"strconv"
	"testing"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/rediscleanup"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	frameredis "github.com/weiloon1234/Foundry-Go/redis"
)

// KeepKeysVariable retains keys even after a passing test when set to "1".
const KeepKeysVariable = rediscleanup.KeepKeysVariable

// Cleanup registers removal of this test's exact application key inventory.
// All connections must use Config(t)'s endpoint. Channels are not keys and are ignored.
// Call before Build or opening clients so application shutdown runs first.
// Use unique test-owned namespaces; failure, panic or KeepKeysVariable=1 retains them.
func Cleanup(t testing.TB, inventory []infrastructure.RedisConnectionInventory) {
	t.Helper()
	var prefixes []string
	for _, connection := range inventory {
		for _, space := range connection.Keys {
			exact, err := rediscleanup.Prefixes(space.Namespace, space.Feature)
			if err != nil || space.Version != keyaddress.Version || len(exact) != 1 || space.Prefix != exact[0] {
				t.Fatal("invalid Redis test cleanup inventory")
			}
			prefixes = append(prefixes, exact[0])
		}
	}
	registerCleanup(t, prefixes)
}

// CleanupNamespace registers removal of exact feature prefixes in a test-owned namespace.
// Include every feature that writes keys, including cache.NamespaceKeyFeature for caches.
// Call before opening clients. It follows the same retention rule as Cleanup.
func CleanupNamespace(t testing.TB, namespace keyspace.Namespace, features ...keyspace.Feature) {
	t.Helper()
	prefixes, err := rediscleanup.Prefixes(namespace, features...)
	if err != nil {
		t.Fatal("invalid Redis test cleanup namespace or feature")
	}
	registerCleanup(t, prefixes)
}

func registerCleanup(t testing.TB, prefixes []string) {
	t.Helper()
	if len(prefixes) == 0 {
		return
	}
	config := Config(t)
	if config.TLS != frameredis.DisableTLS {
		t.Fatal("Redis test cleanup requires a plaintext TCP endpoint")
	}
	options := &driver.Options{
		Network: "tcp", Addr: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Username: config.User, Password: config.Password.Reveal(), DB: config.Database,
		Protocol: 2, MaxRetries: -1, DialerRetries: 1, ContextTimeoutEnabled: true,
		PoolSize: 1, MaxActiveConns: 1,
	}
	rediscleanup.Register(t, func() *driver.Client { return driver.NewClient(options) }, prefixes)
}
