package database

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryCollect lends the stream to a framework decoder until the entire loop
// exits. The capability is internal; scan must not retain or share its borrowed
// Row. The stream mutex excludes public Next/Scan/Close throughout that lifetime.
// Every exit closes rows and a failed collection publishes no count or models.
func (r *Rows) FoundryCollect(seal sqlowner.CollectionSeal, ctx context.Context, scan func(Row) error) (count int, err error) {
	if !seal.Valid() || ctx == nil || scan == nil {
		return 0, safeerror.NewFault(fault.Invalid, "invalid row collection capability or callback")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		err = safeerror.Join("operation failed", err, r.close())
		if err != nil {
			count = 0
		}
	}()
	row := collectedRow{rows: r}
	collect := func() error {
		for r.next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := scan(row); err != nil {
				return err
			}
			count++
		}
		return safeerror.Join("operation failed", r.err, ctx.Err())
	}
	if r.batch == nil {
		err = callback.Isolated("decode model rows", collect)
	} else {
		var driverExit bool
		err, driverExit = r.batch.Run("decode model rows", collect)
		if driverExit {
			r.err = safeerror.Join("operation failed", r.err, r.wrap("iterate", err))
			err = r.err
		}
	}
	if err != nil {
		return 0, err
	}
	return count, nil
}

type collectedRow struct{ rows *Rows }

func (r collectedRow) Scan(destinations ...any) error { return r.rows.scan(destinations) }
func (r collectedRow) FieldEncryption() (context.Context, *encryption.Keyring) {
	return r.rows.FieldEncryption()
}
