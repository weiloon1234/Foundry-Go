// Package rediscleanup owns scoped Redis test cleanup without importing Redis runtime assembly.
package rediscleanup

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/testcleanup"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const KeepKeysVariable = "FOUNDRY_TEST_KEEP_REDIS_KEYS"
const cleanupTimeout = 30 * time.Second
const batchSize = 128

// Prefixes validates exact namespace and feature components before constructing patterns.
func Prefixes(namespace keyspace.Namespace, features ...keyspace.Feature) ([]string, error) {
	if err := namespace.Validate(); err != nil {
		return nil, err
	}
	prefixes := make([]string, 0, len(features))
	for _, feature := range features {
		if !keyspace.ValidName(string(feature)) {
			return nil, errors.New("invalid Redis cleanup feature")
		}
		prefixes = append(prefixes, keyaddress.Prefix(feature, namespace))
	}
	return prefixes, nil
}

// Register snapshots exact prefixes and registers cleanup before clients or apps start.
// newClient runs only during cleanup of a passing test that has not opted to retain keys.
// The returned tracker adds exact prefixes of further test-owned physical addresses.
func Register(t testing.TB, newClient func() *driver.Client, prefixes []string) func(string) {
	t.Helper()
	prefixes = slices.Clone(prefixes)
	for _, prefix := range prefixes {
		parts := strings.Split(prefix, ":")
		if len(parts) != 6 || parts[0] != "foundry" || parts[2] != keyaddress.Version || parts[5] != "" ||
			!keyspace.ValidName(parts[1]) || !keyspace.ValidName(parts[3]) || !keyspace.ValidName(parts[4]) {
			t.Fatal("refusing non-exact Redis test cleanup prefix")
		}
	}
	var mu sync.Mutex
	t.Cleanup(func() {
		mu.Lock()
		prefixes := slices.Clone(prefixes)
		mu.Unlock()
		slices.Sort(prefixes)
		prefixes = slices.Compact(prefixes)
		if len(prefixes) == 0 {
			return
		}
		if testcleanup.Keep(t, KeepKeysVariable) {
			for _, prefix := range prefixes {
				t.Logf("retained Redis test prefix: %s", prefix)
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		client := newClient()
		defer func() {
			if err := client.Close(); err != nil {
				t.Errorf("close Redis cleanup client: %v", safeerror.Wrap("Redis cleanup client close failed", err))
			}
		}()
		for _, prefix := range prefixes {
			if err := remove(ctx, client, prefix); err != nil {
				t.Errorf("clean Redis test prefix %s: %v", prefix, safeerror.Wrap("Redis key cleanup failed", err))
				return
			}
		}
	})
	return func(key string) {
		t.Helper()
		parts := strings.SplitN(key, ":", 6)
		if len(parts) != 6 || parts[0] != "foundry" || parts[2] != keyaddress.Version || parts[5] == "" {
			t.Fatal("invalid owned Redis test address")
		}
		exact, err := Prefixes(keyspace.Namespace{Application: parts[3], Environment: parts[4]}, keyspace.Feature(parts[1]))
		if err != nil {
			t.Fatal("invalid owned Redis test address components")
		}
		mu.Lock()
		defer mu.Unlock()
		if !slices.Contains(prefixes, exact[0]) {
			prefixes = append(prefixes, exact[0])
		}
	}
}

type keyClient interface {
	Scan(context.Context, uint64, string, int64) *driver.ScanCmd
	Unlink(context.Context, ...string) *driver.IntCmd
}

func remove(ctx context.Context, client keyClient, prefix string) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, prefix+"*", batchSize).Result()
		if err != nil {
			return err
		}
		// COUNT is a scan hint, so bound UNLINK independently of the reply size.
		for batch := range slices.Chunk(keys, batchSize) {
			for _, key := range batch {
				if !strings.HasPrefix(key, prefix) {
					return errors.New("Redis scan returned a foreign key")
				}
			}
			if err := client.Unlink(ctx, batch...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
