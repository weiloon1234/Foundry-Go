// Package sqlscope scopes infrastructure writes inside business transactions.
package sqlscope

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// InSchema joins a transaction from the adapter's exact borrowed pool. Its
// savepoint contains both writes and temporary search_path changes. On success
// the original setting is restored; rollback restores it on failure. Callers
// must use child for every operation and propagate any error. Nothing commits.
func InSchema(ctx context.Context, tx *database.Tx, db *database.DB, schema string, fn func(*database.Tx) error) error {
	if ctx == nil || tx == nil || !tx.BelongsTo(db) || !sqlname.Valid(schema) || fn == nil {
		return fault.New(fault.Invalid, "operation requires its database transaction and schema")
	}
	return tx.Savepoint(ctx, func(child *database.Tx) error {
		var previous string
		// ScanOne closes rows, including errors; the next statement never overlaps.
		if err := database.ScanOne(ctx, child, `SELECT pg_catalog.current_setting('search_path')`, nil, &previous); err != nil {
			return err
		}
		if len(previous) > 8192 {
			return fault.New(fault.Invalid, "transaction search path exceeds infrastructure boundary")
		}
		if _, err := child.Exec(ctx, `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		if err := fn(child); err != nil {
			return err
		}
		_, err := child.Exec(ctx, `SELECT pg_catalog.set_config('search_path', $1, true)`, previous)
		return err
	})
}
