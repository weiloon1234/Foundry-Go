package extensions_test

import (
	"context"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresStoreQueuesReadsDuringDatabaseLatency(t *testing.T) {
	f := extensiontest.Open(t, nil)
	registry, err := extensions.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	config := extensions.DefaultConfig()
	config.Schema, config.MaxActive = f.Schema, 2
	store, err := extensions.New(f.DB, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			errs <- store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
				_, err := tx.Exec(ctx, `SELECT pg_sleep(0.05)`)
				return err
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("a slow burst beyond capacity must queue, not fail", err)
		}
	}
}
