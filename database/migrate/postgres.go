package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// PostgresConfig owns a migration history namespace and resource bounds. The
// schema/table must be simple PostgreSQL identifiers of at most 63 ASCII bytes.
// All cooperating runners must use the same history schema and table.
type PostgresConfig struct {
	Schema           string
	Table            string
	LockTimeout      time.Duration
	LockPollInterval time.Duration
	CleanupTimeout   time.Duration
	MaxHistory       int
}

func DefaultPostgresConfig() PostgresConfig {
	return PostgresConfig{Schema: "foundry_ops", Table: "schema_migrations", LockTimeout: 30 * time.Second, LockPollInterval: 50 * time.Millisecond, CleanupTimeout: 5 * time.Second, MaxHistory: 10000}
}

func (c PostgresConfig) Validate() error {
	if !sqlname.Valid(c.Schema) || !sqlname.Valid(c.Table) || len(c.Schema) > 63 || len(c.Table) > 63 || c.LockTimeout <= 0 || c.LockPollInterval <= 0 || c.CleanupTimeout <= 0 || c.MaxHistory <= 0 || c.MaxHistory == math.MaxInt {
		return fault.New(fault.Invalid, "invalid PostgreSQL migration configuration")
	}
	return nil
}

// Postgres is an explicit PostgreSQL migration runner over Foundry's DB. Creating
// it performs no I/O. Status never creates a history table; Up is the only schema
// mutation entry point. There is no reset, wipe, or automatic synchronization.
type Postgres struct {
	db            *database.DB
	registry      *Registry
	config        PostgresConfig
	table         string
	progressTable string
	progressName  string
	lockKey       int64
}

// NewPostgres binds an immutable registry and the intended history namespace.
// The supplied DB must connect to PostgreSQL; protocol fixtures do not certify a
// different database's compatibility with PostgreSQL SQL and advisory locks.
func NewPostgres(db *database.DB, registry *Registry, config PostgresConfig) (*Postgres, error) {
	if db == nil || registry == nil {
		return nil, fault.New(fault.Invalid, "migration runner needs a database and registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	qualified := `"` + config.Schema + `"."` + config.Table + `"`
	// NUL cannot occur in validated identifiers, so components cannot collide.
	hash := sha256.Sum256([]byte("foundry-go:migrations:v1\x00" + config.Schema + "\x00" + config.Table))
	progressName := config.Table[:min(len(config.Table), 37)] + "_progress_" + fmt.Sprintf("%x", hash[:8])
	return &Postgres{db: db, registry: registry, config: config, table: qualified, progressName: progressName, progressTable: `"` + config.Schema + `"."` + progressName + `"`, lockKey: int64(binary.BigEndian.Uint64(hash[:8]))}, nil
}

// Status reads committed history and reports definition drift. A missing table
// produces pending definitions without creating schemas, tables, or locks.
func (p *Postgres) Status(ctx context.Context) (Report, error) {
	history, err := p.history(ctx, p.db)
	if err != nil {
		return Report{}, err
	}
	return p.inspectProgress(ctx, p.db, history)
}

// RunResult preserves migrations known to have committed before a later failure.
// Interrupted names the attempted migration lacking a successful confirmation;
// it may have committed when the returned database outcome is Unknown.
type RunResult struct {
	Batch       int64     `json:"batch"`
	Applied     []Applied `json:"applied"`
	Interrupted *Key      `json:"interrupted,omitempty"`
}

// Up holds one session advisory lock across history validation and all pending
// migrations. Transactional definitions and history commit atomically; explicitly
// nontransactional statements use a durable progress journal. An uncertain step
// blocks further execution until explicit reconciliation. In both modes,
// earlier successful migrations remain committed if a later migration fails.
// A new call inspects history again under the lock. No callback is blindly retried.
func (p *Postgres) Up(ctx context.Context) (result RunResult, err error) {
	err = p.locked(ctx, func(session *database.Session) error {
		if err := p.ensureHistory(ctx, session); err != nil {
			return err
		}
		if err := p.ensureProgress(ctx, session); err != nil {
			return err
		}
		history, err := p.history(ctx, session)
		if err != nil {
			return err
		}
		report, err := p.inspectProgress(ctx, session, history)
		if err != nil {
			return err
		}
		if err := report.Check(); err != nil {
			return err
		}
		var pending []migration
		for _, status := range report.Statuses {
			if status.State == Pending || status.State == Incomplete {
				pending = append(pending, p.registry.ordered[p.registry.byKey[status.Key]])
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if len(pending) > p.config.MaxHistory-len(history) {
			return fault.New(fault.Invalid, "pending migrations would exceed configured history limit")
		}
		if report.LastBatch == math.MaxInt64 {
			return fault.New(fault.Invalid, "migration batch number is exhausted")
		}
		result.Batch = report.LastBatch + 1
		for _, status := range report.Statuses {
			if status.Progress != nil {
				result.Batch = min(result.Batch, status.Progress.Batch)
			}
		}
		for _, item := range pending {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := item.entry.Key
			result.Interrupted = &key
			applied, err := p.apply(ctx, session, item, result.Batch)
			if err != nil {
				return err
			}
			result.Applied = append(result.Applied, applied)
			result.Interrupted = nil
		}
		return nil
	})
	return result, err
}

func (p *Postgres) locked(ctx context.Context, run func(*database.Session) error) error {
	return p.db.Session(ctx, func(session *database.Session) (err error) {
		if err := p.acquireLock(ctx, session); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, p.releaseLock(ctx, session)) }()
		return run(session)
	})
}

func (p *Postgres) acquireLock(ctx context.Context, session *database.Session) error {
	wait, cancel := context.WithTimeout(ctx, p.config.LockTimeout)
	defer cancel()
	for {
		var acquired bool
		if err := database.ScanOne(wait, session, "SELECT pg_catalog.pg_try_advisory_lock($1)", []any{p.lockKey}, &acquired); err != nil {
			return err
		}
		if acquired {
			return nil
		}
		timer := time.NewTimer(p.config.LockPollInterval)
		select {
		case <-timer.C:
		case <-wait.Done():
			timer.Stop()
			return fault.Wrap(fault.Timeout, "migration lock acquisition timed out", wait.Err())
		}
	}
}

func (p *Postgres) releaseLock(ctx context.Context, session *database.Session) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.CleanupTimeout)
	defer cancel()
	var released bool
	err := database.ScanOne(cleanup, session, "SELECT pg_catalog.pg_advisory_unlock($1)", []any{p.lockKey}, &released)
	if err == nil && !released {
		err = fault.New(fault.Conflict, "migration session did not own its expected advisory lock")
	}
	if err != nil {
		session.Discard()
	}
	return err
}

func (p *Postgres) ensureHistory(ctx context.Context, session *database.Session) error {
	if _, err := session.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS "`+p.config.Schema+`"`); err != nil {
		return err
	}
	_, err := session.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+p.table+` (
origin text NOT NULL,
id text NOT NULL,
version text NOT NULL,
checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
batch bigint NOT NULL CHECK (batch > 0),
applied_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
PRIMARY KEY (origin, id)
)`)
	return err
}

func (p *Postgres) history(ctx context.Context, executor database.Executor) ([]Applied, error) {
	var kind string
	err := database.ScanOne(ctx, executor, `SELECT c.relkind::text FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2`, []any{p.config.Schema, p.config.Table}, &kind)
	if errors.Is(err, database.NotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if kind != "r" {
		return nil, fault.New(fault.Invalid, "migration history relation must be an ordinary table")
	}
	var records []Applied
	err = database.ForEach(ctx, executor, "SELECT origin, id, version, checksum, batch, applied_at FROM "+p.table+" ORDER BY batch, origin, id LIMIT $1", []any{p.config.MaxHistory + 1}, func(row database.Row) (Applied, error) {
		var entry Applied
		var checksum string
		if err := row.Scan(&entry.Key.Origin, &entry.Key.ID, &entry.Version, &checksum, &entry.Batch, &entry.AppliedAt); err != nil {
			return Applied{}, err
		}
		parsed, err := ParseChecksum(checksum)
		if err != nil {
			return Applied{}, err
		}
		entry.Checksum = parsed
		entry.AppliedAt = entry.AppliedAt.UTC()
		return entry, nil
	}, func(entry Applied) error {
		if len(records) >= p.config.MaxHistory {
			return fault.New(fault.Invalid, "migration history exceeds configured entry limit")
		}
		records = append(records, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (p *Postgres) apply(ctx context.Context, session *database.Session, item migration, batch int64) (Applied, error) {
	if item.entry.Mode == NonTransactional {
		return p.applyNonTransactional(ctx, session, item, batch)
	}
	entry := Applied{Key: item.entry.Key, Version: item.entry.Version, Checksum: item.entry.Checksum, Batch: batch}
	err := session.Transaction(ctx, func(tx *database.Tx) error {
		for _, statement := range item.statements {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return database.ScanOne(ctx, tx, "INSERT INTO "+p.table+" (origin, id, version, checksum, batch) VALUES ($1, $2, $3, $4, $5) RETURNING applied_at", []any{string(entry.Key.Origin), string(entry.Key.ID), string(entry.Version), entry.Checksum.String(), entry.Batch}, &entry.AppliedAt)
	})
	if err != nil {
		return Applied{}, err
	}
	entry.AppliedAt = entry.AppliedAt.UTC()
	return entry, nil
}
