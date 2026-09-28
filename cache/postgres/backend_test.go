package postgres_test

import (
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

func prepare(t *testing.T) (*cachepg.Backend, *testkit.Clock) {
	t.Helper()
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	source := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
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
	c := cachepg.DefaultConfig()
	c.Schema = schema
	c.Clock = source
	b, err := cachepg.New(db, c)
	if err != nil {
		t.Fatal(err)
	}
	return b, source
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
