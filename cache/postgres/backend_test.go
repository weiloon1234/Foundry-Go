package postgres_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/cache"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
	"time"
)

// migrated applies the cache migrations to a fresh isolated schema.
func migrated(t *testing.T) (*database.DB, string) {
	t.Helper()
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	definitions := cachepg.Migrations()
	if _, err := migrate.New(definitions...); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, d := range definitions {
			for _, sql := range d.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return db, schema
}
func open(t *testing.T, db *database.DB, c cachepg.Config) *cachepg.Backend {
	t.Helper()
	b, err := cachepg.New(db, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return b
}
func prepare(t *testing.T) (*cachepg.Backend, *testkit.Clock) {
	t.Helper()
	db, schema := migrated(t)
	source := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	c := cachepg.DefaultConfig()
	c.Schema = schema
	c.Clock = source
	return open(t, db, c), source
}

// databaseClock uses PostgreSQL's clock for expiry, as production does.
func databaseClock(t *testing.T) *cachepg.Backend {
	t.Helper()
	db, schema := migrated(t)
	c := cachepg.DefaultConfig()
	c.Schema = schema
	return open(t, db, c)
}
func rows(t *testing.T, db *database.DB, schema string) int64 {
	t.Helper()
	result, err := db.Query(t.Context(), `SELECT count(*) FROM "`+schema+`".foundry_cache_entries`)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	var count int64
	if !result.Next() {
		t.Fatal("missing count", result.Err())
	}
	if err := result.Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func key(t *testing.T, s string) cache.EntryKey {
	t.Helper()
	key, err := cache.NewEntryKey(cache.Namespace{Application: "postgres-contract", Environment: "test"}, "values", s)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func TestSharedCacheContract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cachetest.Backend, func(string) cache.EntryKey) {
		b, _ := prepare(t)
		return b, func(s string) cache.EntryKey { return key(t, s) }
	})
}
func TestExpiryAndBoundedPruning(t *testing.T) {
	b, source := prepare(t)
	k := key(t, "expiry")
	if err := b.Put(t.Context(), k, []byte("1"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Expire(t.Context(), k, cache.For(2*time.Second)); err != nil || !ok {
		t.Fatal(ok, err)
	}
	source.Advance(3 * time.Second)
	if ok, err := b.Exists(t.Context(), k); err != nil || ok {
		t.Fatal(ok, err)
	}
	if got, err := b.Increment(t.Context(), k, 5, cache.For(time.Second)); err != nil || got != 5 {
		t.Fatal(got, err)
	}
	source.Advance(2 * time.Second)
	if count, err := b.Prune(t.Context(), 1); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := b.Prune(t.Context(), cachepg.MaxPrune+1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestSharedEntryContract(t *testing.T) {
	cachetest.RunBasicEntries(t, func(t *testing.T) (cachetest.BasicEntryBackend, func(string) cache.EntryKey) {
		b, _ := prepare(t)
		return b, func(s string) cache.EntryKey { return key(t, s) }
	})
}
func TestDatabaseClockContracts(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) (cachetest.Backend, func(string) cache.EntryKey) {
		return databaseClock(t), func(s string) cache.EntryKey { return key(t, s) }
	})
	cachetest.RunBasicEntries(t, func(t *testing.T) (cachetest.BasicEntryBackend, func(string) cache.EntryKey) {
		return databaseClock(t), func(s string) cache.EntryKey { return key(t, s) }
	})
}
func TestDatabaseClockExpiry(t *testing.T) {
	b := databaseClock(t)
	k := key(t, "database-clock")
	if err := b.Put(t.Context(), k, []byte("live"), cache.For(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if data, hit, err := b.Get(t.Context(), k); err != nil || !hit || string(data) != "live" {
		t.Fatal(string(data), hit, err)
	}
	// Expiry is computed by PostgreSQL at write time; the next statement's
	// clock is always later than one microsecond.
	if err := b.Put(t.Context(), k, []byte("short"), cache.For(time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := b.Get(t.Context(), k); err != nil || hit {
		t.Fatal("expired row read", hit, err)
	}
	if changed, err := b.Expire(t.Context(), k, cache.Forever()); err != nil || changed {
		t.Fatal("expired row revived", changed, err)
	}
	if added, err := b.Add(t.Context(), k, []byte("added"), cache.For(time.Microsecond)); err != nil || !added {
		t.Fatal("expired row blocked add", added, err)
	}
	if value, err := b.Increment(t.Context(), k, 4, cache.For(time.Hour)); err != nil || value != 4 {
		t.Fatal("expired row kept its count", value, err)
	}
	if count, err := b.Prune(t.Context(), cachepg.MaxPrune); err != nil || count != 0 {
		t.Fatal("live row pruned", count, err)
	}
	if err := b.Put(t.Context(), key(t, "other"), nil, cache.For(time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if count, err := b.Prune(t.Context(), cachepg.MaxPrune); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestPersistentContract(t *testing.T) {
	cachetest.RunPersistent(t, func(t *testing.T) cachetest.PersistentFixture {
		db, schema := migrated(t)
		source := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
		return cachetest.PersistentFixture{
			Open: func(t *testing.T, maxEntries, maxValueBytes int) cachetest.PersistentBackend {
				c := cachepg.DefaultConfig()
				c.Schema = schema
				c.Clock = source
				c.MaxEntries = maxEntries
				c.MaxValueBytes = maxValueBytes
				return open(t, db, c)
			},
			Clock: source,
			Key: func(namespace cache.Namespace, logical string) cache.EntryKey {
				k, err := cache.NewEntryKey(namespace, "values", logical)
				if err != nil {
					t.Fatal(err)
				}
				return k
			},
		}
	})
}
func TestStartIsLazyAndOwnsThePruner(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	c := cachepg.DefaultConfig()
	c.Schema = schema
	c.PruneInterval = time.Second
	source := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	c.Clock = source
	b := open(t, db, c)
	// The table is migrated after Start, as an application may do at boot.
	if err := b.Start(t.Context()); err != nil {
		t.Fatal("start performed I/O before migration", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, d := range cachepg.Migrations() {
			for _, sql := range d.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(t.Context(), key(t, "expiring"), []byte("value"), cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	source.Advance(2 * time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for rows(t, db, schema) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("started backend did not prune")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal("close did not stop the pruner", err)
	}
	select {
	case <-b.Done():
	default:
		t.Fatal("closed backend is not done")
	}
	if _, _, err := b.Get(t.Context(), key(t, "expiring")); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	if err := b.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal("closed backend restarted", err)
	}
}
func TestPruneIntervalValidation(t *testing.T) {
	c := cachepg.DefaultConfig()
	if c.PruneInterval != time.Minute || c.Clock != nil {
		t.Fatal(c.PruneInterval, c.Clock)
	}
	for interval, valid := range map[time.Duration]bool{0: true, time.Second: true, 24 * time.Hour: true, -time.Second: false, time.Millisecond: false, 25 * time.Hour: false} {
		c.PruneInterval = interval
		if err := c.Validate(); (err == nil) != valid {
			t.Fatal(interval, err)
		}
	}
}
