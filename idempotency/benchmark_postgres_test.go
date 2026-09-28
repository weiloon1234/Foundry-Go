package idempotency

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"strings"
	"testing"
	"time"
)

func BenchmarkIdempotentPostgres(b *testing.B) {
	scope, _ := testDatabase(b)
	var metrics driverMetrics
	config := scope.Config()
	adapter, err := postgres.New(config)
	if err != nil {
		b.Fatal(err)
	}
	adapter.Connector = acknowledgmentConnector{Connector: adapter.Connector, metrics: &metrics}
	db, err := database.Open(b.Context(), adapter, config.Pool)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close(context.Background()) })
	store := testStore(b, db, scope.Schema(), func(c *Config) { c.MaxRetainedPerCaller = 100000; c.DuplicateWait = time.Second })
	identity := testScope(b, "cost")
	for _, mode := range []string{"New", "Replay", "Contended"} {
		b.Run(mode, func(b *testing.B) {
			op := testOperation(b, store, OperationID("cost."+strings.ToLower(mode)))
			// Go repeats a sub-benchmark in the same retained schema for -count.
			// Every sample owns a fresh key namespace, including its warm-up.
			prefix := "benchmark-" + rand.Text() + "-"
			key := testKey(b, prefix+"replay")
			input := testInput{"cost"}
			result, err := op.Run(b.Context(), identity, key, input, effect)
			if err != nil {
				b.Fatal(err)
			}
			storage := len(result.Encoded())
			// Warm the conflict/reload statement before measuring normal retained replay.
			if _, err := op.Run(b.Context(), identity, key, input, effect); err != nil {
				b.Fatal(err)
			}
			metrics.commands.Store(0)
			metrics.resets.Store(0)
			metrics.transactionNanos.Store(0)
			count := 0
			b.ReportAllocs()
			for b.Loop() {
				count++
				if mode != "Replay" {
					key = testKey(b, fmt.Sprintf("%s%016d", prefix, count))
				}
				if mode == "Contended" {
					entered, release := make(chan struct{}), make(chan struct{})
					winner := make(chan error, 1)
					duplicate := make(chan error, 1)
					go func() {
						_, err := op.Run(b.Context(), identity, key, input, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
							out, err := effect(ctx, tx, in)
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
								return testOutput{}, ctx.Err()
							}
							return out, err
						})
						winner <- err
					}()
					select {
					case <-entered:
					case err := <-winner:
						b.Fatalf("winner never entered its callback: %v", err)
					case <-b.Context().Done():
						b.Fatal("winner did not reach contention barrier")
					}
					go func() {
						result, err := op.Run(b.Context(), identity, key, input, effect)
						if err == nil && !result.Replayed() {
							err = fmt.Errorf("duplicate did not replay")
						}
						duplicate <- err
					}()
					time.Sleep(time.Millisecond)
					close(release)
					if err := <-winner; err != nil {
						b.Fatal(err)
					}
					if err := <-duplicate; err != nil {
						b.Fatal(err)
					}
				} else {
					result, err := op.Run(b.Context(), identity, key, input, effect)
					if err != nil || !result.Committed() {
						b.Fatal(err)
					}
				}
			}
			requests := float64(count)
			perIteration := float64(1)
			if mode == "Contended" {
				requests *= 2
				perIteration = 2
			}
			b.ReportMetric(perIteration, "requests/op")
			b.ReportMetric(float64(metrics.commands.Load())/requests, "SQL-commands/request")
			b.ReportMetric(float64(metrics.resets.Load())/requests, "checkouts/request")
			b.ReportMetric(float64(metrics.transactionNanos.Load())/requests/1e3, "transaction-us/request")
			b.ReportMetric(float64(storage), "stored-B/result")
		})
	}
}
