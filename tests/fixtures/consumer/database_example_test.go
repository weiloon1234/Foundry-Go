package consumer_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/database"
)

// Direct pool callers can opt into the same safe startup diagnostics that
// Modules inherit automatically. The returned pool belongs to the caller.
func Example_databaseStartup() {
	open := func(ctx context.Context, adapter database.Adapter, logger *slog.Logger) (*database.DB, error) {
		return database.Open(ctx, adapter, database.DefaultPoolConfig(), database.WithStartupLog(logger))
	}
	_ = open
}

var (
	_ database.Executor = (*database.DB)(nil)
	_ database.Executor = (*database.Tx)(nil)
)

// This consumer example is compile-checked. A PostgreSQL-connected instance is
// required to execute it; real driver acceptance belongs to milestone 04's gate.
func Example_databaseTransaction() {
	run := func(ctx context.Context, db *database.DB, expected int64) error {
		return db.Transaction(ctx, func(tx *database.Tx) error {
			var actual int64
			if err := database.ScanOne(ctx, tx, "SELECT $1::bigint", []any{expected}, &actual); err != nil {
				return err
			}
			if actual != expected {
				return errors.New("unexpected database value")
			}
			return tx.AfterCommit(func(context.Context) error {
				fmt.Println("committed", actual)
				return nil
			})
		}, database.TxOptions{Isolation: database.ReadCommitted})
	}
	_ = run
}
