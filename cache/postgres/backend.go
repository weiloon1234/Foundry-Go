// Package postgres implements persistent cache using an explicitly migrated
// PostgreSQL table and borrowed Foundry connection. It never creates schema at
// application boot. Tags, batches and distributed fill leases are not supplied.
package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

const MaxPrune = 256

// Config bounds one shared table. Processes sharing a schema must use the same
// bounds and synchronized clocks. Expired rows count until explicitly pruned.
type Config struct {
	Schema        string
	MaxEntries    int
	MaxBytes      int64
	MaxValueBytes int
	Clock         clock.Clock
}

func DefaultConfig() Config {
	limits := cacheatomic.DefaultLimits()
	return Config{Schema: "public", MaxEntries: limits.MaxEntries, MaxBytes: limits.MaxBytes, MaxValueBytes: limits.MaxValueBytes, Clock: clock.System{}}
}
func (c Config) Validate() error {
	if err := (cacheatomic.Limits{MaxEntries: c.MaxEntries, MaxBytes: c.MaxBytes, MaxValueBytes: c.MaxValueBytes}).Validate(); err != nil {
		return err
	}
	if !sqlname.Valid(c.Schema) || credential.IsNil(c.Clock) {
		return fault.New(fault.Invalid, "invalid PostgreSQL cache configuration")
	}
	return nil
}

type Backend struct {
	*cacheatomic.Backend
	db     *database.DB
	config Config
	table  string
}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "PostgreSQL cache requires a database")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Backend{db: db, config: config, table: `"` + config.Schema + `".foundry_cache_entries`}
	var err error
	b.Backend, err = cacheatomic.New(b, config.Clock, config.MaxValueBytes)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// within serializes table capacity and key changes in a transaction-scoped lock.
// All reads use the primary so read-after-write and expiry remain authoritative.
// Raw SQL is confined to this atomic adapter boundary; inputs are parameters.
func (b *Backend) within(ctx context.Context, fn func(*database.Tx) error) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "PostgreSQL cache requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "foundry.cache:"+b.config.Schema); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
}
func (b *Backend) Access(ctx context.Context, key cache.EntryKey, change cacheatomic.Change) error {
	return b.within(ctx, func(tx *database.Tx) error {
		hash := sha256.Sum256([]byte(key.String()))
		id := hex.EncodeToString(hash[:])
		rows, err := tx.Query(ctx, `SELECT address, CASE WHEN octet_length(payload) <= $2 THEN payload ELSE NULL END, octet_length(payload), expires_at FROM `+b.table+` WHERE key = $1`, id, b.config.MaxValueBytes)
		if err != nil {
			return err
		}
		var old *cacheatomic.Record
		if rows.Next() {
			var address string
			var data []byte
			var size int
			var expiry sql.NullTime
			if err = rows.Scan(&address, &data, &size, &expiry); err != nil {
				_ = rows.Close()
				return err
			}
			if address != key.String() || size > b.config.MaxValueBytes || size != len(data) {
				_ = rows.Close()
				return fault.New(fault.Invalid, "invalid PostgreSQL cache record")
			}
			old = &cacheatomic.Record{Data: data}
			if expiry.Valid {
				old.Expires = expiry.Time
			}
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		next, update, err := change(old)
		if err != nil || !update {
			return err
		}
		if next == nil {
			_, err = tx.Exec(ctx, `DELETE FROM `+b.table+` WHERE key=$1`, id)
			return err
		}
		// Enforce quota before replacing a live record, preserving it on rejection.
		budget, err := tx.Query(ctx, `SELECT count(*), COALESCE(sum(octet_length(address)+octet_length(payload)),0)::bigint FROM `+b.table+` WHERE key <> $1`, id)
		if err != nil {
			return err
		}
		var count, bytes int64
		if !budget.Next() {
			err = errors.Join(budget.Err(), budget.Close())
			if err != nil {
				return err
			}
			return fault.New(fault.Invalid, "missing PostgreSQL cache capacity result")
		}
		err = budget.Scan(&count, &bytes)
		err = errors.Join(err, budget.Close())
		if err != nil {
			return err
		}
		cost := int64(len(key.String())) + int64(len(next.Data))
		if count >= int64(b.config.MaxEntries) || cost > b.config.MaxBytes || bytes > b.config.MaxBytes-cost {
			return fault.New(fault.Invalid, "PostgreSQL cache capacity reached; prune required")
		}
		var expiry any
		if !next.Expires.IsZero() {
			expiry = next.Expires.UTC()
		}
		data := next.Data
		if data == nil {
			data = []byte{}
		}
		_, err = tx.Exec(ctx, `INSERT INTO `+b.table+` (key,address,payload,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT (key) DO UPDATE SET address=EXCLUDED.address,payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, id, key.String(), data, expiry)
		return err
	})
}

// Prune deletes at most limit expired rows in this configured cache table.
func (b *Backend) Prune(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid cache prune limit")
	}
	var removed int
	err := b.within(ctx, func(tx *database.Tx) error {
		result, err := tx.Exec(ctx, `DELETE FROM `+b.table+` WHERE key IN (SELECT key FROM `+b.table+` WHERE expires_at <= $1 ORDER BY expires_at,key LIMIT $2)`, b.config.Clock.Now(), limit)
		removed = int(result.RowsAffected)
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// Inspect checks metadata and size without selecting the payload. Expiry changes
// remain atomic with all other operations under the same transaction lock.
func (b *Backend) Inspect(ctx context.Context, key cache.EntryKey, inspect cacheatomic.Inspection) (bool, error) {
	found := false
	err := b.within(ctx, func(tx *database.Tx) error {
		hash := sha256.Sum256([]byte(key.String()))
		id := hex.EncodeToString(hash[:])
		rows, err := tx.Query(ctx, `SELECT address,octet_length(payload),expires_at FROM `+b.table+` WHERE key=$1`, id)
		if err != nil {
			return err
		}
		if !rows.Next() {
			return errors.Join(rows.Err(), rows.Close())
		}
		var address string
		var size int
		var expiry sql.NullTime
		err = rows.Scan(&address, &size, &expiry)
		err = errors.Join(err, rows.Close())
		if err != nil {
			return err
		}
		if address != key.String() || size > b.config.MaxValueBytes {
			return fault.New(fault.Invalid, "invalid PostgreSQL cache record")
		}
		expires, live, err := inspect(expiry.Time)
		found = live
		if err != nil || !found || expires == nil {
			return err
		}
		var next any
		if !expires.IsZero() {
			next = expires.UTC()
		}
		_, err = tx.Exec(ctx, `UPDATE `+b.table+` SET expires_at=$2 WHERE key=$1`, id, next)
		return err
	})
	return found && err == nil, err
}
