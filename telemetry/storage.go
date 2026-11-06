package telemetry

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type storage struct {
	collector *Collector
	schema    string
	config    Config
	horizon   time.Duration
	gate      chan struct{}
	transact  func(context.Context, func(*database.Tx) error, ...database.TxOptions) error
	applied   model.ID[batch]
	progress  receiptState
}

func newStorage(c *Collector, db *database.DB, schema string, config Config) (*storage, error) {
	if c == nil || db == nil || !sqlname.Valid(schema) {
		return nil, invalid("telemetry storage requires a collector, database and explicit schema")
	}
	config.Alerts.Rules = slices.Clone(config.Alerts.Rules)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	attempt := max(config.FlushTimeout, config.DrainTimeout)
	if attempt > maxRetention-2*c.config.PendingRetention {
		return nil, invalid("telemetry retry horizon exceeds retention")
	}
	horizon := 2*c.config.PendingRetention + attempt
	if config.Retention.Hour <= horizon {
		return nil, invalid("telemetry run retention must exceed the receipt retry horizon")
	}
	if err := orm.Check(storageTables()...); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.storageOwned {
		return nil, safeerror.NewFault(fault.Conflict, "telemetry collector already has a storage owner")
	}
	c.storageOwned = true
	return &storage{collector: c, schema: schema, config: config, horizon: horizon, gate: make(chan struct{}, 1), transact: db.Primary().Transaction}, nil
}

func (s *storage) enter(ctx context.Context) error {
	if ctx == nil {
		return invalid("telemetry operation requires a context")
	}
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type persistResult struct {
	state              receiptState
	committed, expired bool
	uncertain, absent  bool
}

// flush consumes only the FIFO head. The future incident evaluator supplies its
// next bounded rule state; receipt replay always uses the committed state instead.
// Empty queues are a no-op. A committed-but-failed completion advances the queue
// and returns its error; an unknown completion retains the same batch identity.
func (s *storage) flush(ctx context.Context, next []ruleProgress) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer func() { <-s.gate }()
	c := s.collector
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return nil
	}
	b := c.pending[0]
	c.activeBatch = b.id
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.activeBatch = model.ID[batch]{}; c.mu.Unlock() }()
	staged := receiptState{Rules: slices.Clone(next), Counters: slices.Clone(b.runtime.Samples), Dropped: b.runtime.Dropped, StorageFailures: b.runtime.StorageFailures}
	if err := staged.validate(); err != nil {
		return err
	}
	attempt, cancel := context.WithTimeout(ctx, s.config.FlushTimeout)
	defer cancel()
	result, err := s.persist(attempt, b, staged)
	c.mu.Lock()
	defer c.mu.Unlock()
	if result.absent || result.committed {
		c.uncertainBatch = model.ID[batch]{}
	}
	if result.uncertain {
		c.uncertainBatch = b.id
	}
	if result.committed {
		if s.applied != b.id {
			c.dropLocked(result.state.StorageDropped)
			s.progress = result.state.clone()
			s.applied = b.id
		}
		c.pending[0] = batch{}
		c.pending = c.pending[1:]
	} else if result.expired {
		c.dropBatchLocked(b)
		c.pending[0] = batch{}
		c.pending = c.pending[1:]
	}
	if err != nil {
		c.failures = saturatingAdd(c.failures, 1)
	}
	return err
}

func (c *Collector) dropBatchLocked(b batch) {
	c.dropLocked(uint64(len(b.issues)))
	for _, entry := range b.buckets {
		if entry.Interval == time.Minute {
			c.dropLocked(entry.Metric.Count)
		}
	}
}

func (s *storage) expired(b batch) bool {
	return s.collector.elapsed().Sub(b.admitted) >= s.collector.config.PendingRetention
}

func (s *storage) within(ctx context.Context, fn func(*database.Tx) error) (*database.Tx, error) {
	var used *database.Tx
	err := s.transact(ctx, func(tx *database.Tx) error {
		used = tx
		// Schema selection has no typed row equivalent. All row operations below
		// use declarations; a validated identifier cannot change this SQL shape.
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		if _, err := orm.RequireLocked(ctx, tx, orm.Lock(store.Controls.Query()).Where(orm.KeyEq(store.Controls.Key(), "storage"))); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
	return used, safeerror.Prefix("telemetry storage", err)
}

func serverTime(ctx context.Context, tx *database.Tx) (temporal.DateTime, error) {
	var instant time.Time
	// TransactionTime is the start of the transaction, before a possible lock
	// wait. No typed expression provides the current PostgreSQL wall clock.
	if err := database.ScanOne(ctx, tx, `SELECT pg_catalog.clock_timestamp()`, nil, &instant); err != nil {
		return temporal.DateTime{}, err
	}
	return temporal.NewDateTime(instant)
}

func (s *storage) persist(ctx context.Context, b batch, staged receiptState) (persistResult, error) {
	result := persistResult{}
	if err := staged.validate(); err != nil {
		return result, err
	}
	tx, err := s.within(ctx, func(tx *database.Tx) error {
		prior, err := orm.First(ctx, tx, flushes.Query().Where(orm.KeyEq(flushes.Key(), receiptKey(b.id))))
		if err != nil {
			return err
		}
		if receipt, ok := prior.Get(); ok {
			if receipt.RunID != s.runKey() {
				return corruptPayload()
			}
			decoded, err := decodePayload(receipt.Result, maxReceiptPayloadBytes)
			if err != nil {
				return err
			}
			if err = decoded.validate(); err != nil {
				return err
			}
			// A visible receipt is evidence of a prior commit even if this read
			// transaction later encounters a completion failure.
			result.state, result.committed = decoded, true
			return nil
		}
		result.absent = true
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.expired(b) {
			result.expired = true
			return nil
		}
		now, err := serverTime(ctx, tx)
		if err != nil {
			return err
		}
		count, err := orm.Count(ctx, tx, flushes.Query())
		if err != nil {
			return err
		}
		if count >= int64(s.config.Retention.MaxReceiptRows) {
			return safeerror.NewFault(fault.Overloaded, "telemetry receipt capacity reached")
		}
		if err = s.writeRun(ctx, tx, b, now); err != nil {
			return err
		}
		metricLoss, err := s.writeMetrics(ctx, tx, b)
		if err != nil {
			return err
		}
		issueLoss, err := s.writeIssues(ctx, tx, b)
		if err != nil {
			return err
		}
		staged.StorageDropped = saturatingAdd(metricLoss, issueLoss)
		payload, err := encodePayload(staged, maxReceiptPayloadBytes)
		if err != nil {
			return err
		}
		until, err := now.Add(s.horizon)
		if err != nil {
			return err
		}
		if _, err = orm.Insert(ctx, tx, flushes.Table, orm.KeySet(flushes.Key(), receiptKey(b.id)), orm.ColumnSet(flushes.RunID, s.runKey()), orm.TimeSet(flushes.CreatedAt, b.at), orm.TimeSet(flushes.StoredAt, now), orm.TimeSet(flushes.RetainUntil, until), orm.JSONSet(flushes.Result, payload)); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if s.expired(b) {
			return safeerror.NewFault(fault.Overloaded, "telemetry batch expired during storage")
		}
		result.state = staged.clone()
		return nil
	})
	if !result.expired && tx != nil && tx.State() == database.TxCommitted {
		result.committed = true
	}
	result.uncertain = !result.committed && !result.expired && tx != nil && tx.State() == database.TxUnknown
	if err == nil && !result.committed && !result.expired {
		err = database.NewError("telemetry flush", database.CommitUnknown)
	}
	return result, err
}

func (s *storage) writeRun(ctx context.Context, tx *database.Tx, b batch, now temporal.DateTime) error {
	exists, err := orm.Exists(ctx, tx, runs.Query().Where(orm.KeyEq(runs.Key(), s.runKey())))
	if err != nil {
		return err
	}
	if !exists {
		count, err := orm.Count(ctx, tx, runs.Query())
		if err != nil {
			return err
		}
		if count >= int64(s.config.Retention.MaxRuns) {
			return safeerror.NewFault(fault.Overloaded, "telemetry run capacity reached")
		}
	}
	payload, err := encodePayload(b.runtimeSnapshot(), maxRuntimePayloadBytes)
	if err != nil {
		return err
	}
	until, err := now.Add(s.config.Retention.Hour)
	if err != nil {
		return err
	}
	q := runs.Query()
	cr := rows.ConflictRowsOf(q)
	stored := expr.At(cr.Stored(), orm.TimeExpr(runs.RetainUntil))
	proposed := expr.At(cr.Proposed(), orm.TimeExpr(runs.RetainUntil))
	conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(runs.Key()))).DoUpdate(orm.ConflictIncoming(orm.TimeExpr(runs.HeartbeatAt)), orm.ConflictIncoming(orm.JSONExpr(runs.Runtime)), rows.SetConflictValue(orm.TimeExpr(runs.RetainUntil), expr.When(expr.Greater(proposed, stored), proposed).Else(stored)))
	_, err = orm.Upsert(ctx, tx, q, conflict, orm.KeySet(runs.Key(), s.runKey()), orm.TextSet(runs.Instance, s.collector.config.Instance), orm.TextSet(runs.Release, s.collector.config.Release), orm.TimeSet(runs.StartedAt, s.collector.started), orm.TimeSet(runs.HeartbeatAt, b.at), orm.TimeSet(runs.RetainUntil, until), orm.JSONSet(runs.Runtime, payload))
	return err
}

// Row identities are decoded at this boundary so every ORM primary key belongs
// to its persisted model; the UUID bytes remain the collector receipt/run identity.
func (s *storage) runKey() model.ID[runRow] {
	return model.IDFromBytes[runRow](s.collector.runID.Bytes())
}
func receiptKey(id model.ID[batch]) model.ID[flushRow] {
	return model.IDFromBytes[flushRow](id.Bytes())
}
