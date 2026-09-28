// Package outboxtest owns isolated, retained schemas for publication acceptance.
package outboxtest

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/outbox"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Writer always commits an outer transaction after selecting its test schema.
type Writer struct {
	DB     *database.DB
	Schema string
}

func (w Writer) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return w.DB.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+w.Schema+`"`); err != nil {
			return err
		}
		return fn(tx)
	}, options...)
}
func Open(t *testing.T) Writer {
	t.Helper()
	db := pgtest.Open(t)
	writer := Writer{DB: db, Schema: pgtest.Namespace(t, db)}
	if err := writer.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, definition := range outbox.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return writer
}
