package telemetry

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// telemetryAckConnector changes only one real PostgreSQL COMMIT acknowledgment.
// Its driver forwarding follows the existing idempotency lost-ack fixture.
// Both branches return the same unknown result to the database owner; only the
// server's resulting receipt distinguishes committed from absent work.
type telemetryAckConnector struct {
	driver.Connector
	armed    *atomic.Bool
	rollback bool
}

func (c telemetryAckConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &telemetryAckConn{Conn: conn, armed: c.armed, rollback: c.rollback}, nil
}

type telemetryAckConn struct {
	driver.Conn
	armed    *atomic.Bool
	rollback bool
}

func (c *telemetryAckConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &telemetryAckTx{Tx: tx, armed: c.armed, rollback: c.rollback}, nil
}
func (c *telemetryAckConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
func (c *telemetryAckConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}
func (c *telemetryAckConn) ResetSession(ctx context.Context) error {
	return c.Conn.(driver.SessionResetter).ResetSession(ctx)
}
func (c *telemetryAckConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *telemetryAckConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}
func (c *telemetryAckConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type telemetryAckTx struct {
	driver.Tx
	armed    *atomic.Bool
	rollback bool
}

func (tx *telemetryAckTx) Commit() error {
	if tx.armed.Swap(false) {
		if tx.rollback {
			if err := tx.Tx.Rollback(); err != nil {
				return err
			}
		} else {
			if err := tx.Tx.Commit(); err != nil {
				return err
			}
		}
		return errors.New("injected private telemetry commit acknowledgment failure")
	}
	return tx.Tx.Commit()
}

func commitAckDatabase(t *testing.T, schema string, rollback bool) (*database.DB, *atomic.Bool) {
	t.Helper()
	config := pgtest.Config(t)
	config.Schema = schema
	config.Pool.MaxOpen, config.Pool.MaxIdle = 2, 2
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	armed := &atomic.Bool{}
	adapter.Connector = telemetryAckConnector{Connector: adapter.Connector, armed: armed, rollback: rollback}
	db, err := database.Open(t.Context(), adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return db, armed
}

func TestPostgresTelemetryUnknownCommitReconcilesReceiptWithoutDuplicateWork(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		for _, expire := range []bool{false, true} {
			name := "committed"
			if rollback {
				name = "absent"
			}
			if expire {
				name += "-expired"
			}
			t.Run(name, func(t *testing.T) {
				s, assertions, schema, clock := postgresStorage(t, collectorConfig(), engineConfig())
				faultDB, armed := commitAckDatabase(t, schema, rollback)
				var afterCommit atomic.Int32
				writer := faultDB.Primary().Transaction
				s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
					return writer(ctx, func(tx *database.Tx) error {
						if err := fn(tx); err != nil {
							return err
						}
						return tx.AfterCommit(func(context.Context) error { afterCommit.Add(1); return nil })
					}, options...)
				}
				c := s.collector
				c.ObserveRequest(t.Context(), request())
				if err := c.Report(t.Context(), backendReport()); err != nil {
					t.Fatal(err)
				}
				b := capture(t, c)
				original := []ruleProgress{{Rule: RuleLatency, Bad: 2}}
				armed.Store(true)
				if err := s.flush(t.Context(), original); !errors.Is(err, database.CommitUnknown) {
					t.Fatalf("lost acknowledgment = %v", err)
				}
				if afterCommit.Load() != 0 {
					t.Fatal("lost acknowledgment unexpectedly executed after-commit callback")
				}
				if len(c.pending) != 1 || c.pending[0].id != b.id || !s.applied.IsZero() || len(s.progress.Rules) != 0 {
					t.Fatal("unknown result advanced or replaced FIFO head")
				}
				want := telemetryCounts{runs: 1, receipts: 1, metrics: 2, issues: 1, events: 1}
				if rollback {
					want = telemetryCounts{}
				}
				if got := countTelemetry(t, assertions, schema); got != want {
					t.Fatalf("server outcome = %+v, want %+v", got, want)
				}
				if expire {
					clock.advance(c.config.PendingRetention + time.Nanosecond)
					capture(t, c)
					if c.pending[0].id != b.id {
						t.Fatal("capture expired an unresolved receipt identity")
					}
				}
				// The original after-commit callback is deliberately unavailable.
				// Only the receipt payload can recover the committed progress.
				s.transact = writer
				replacement := []ruleProgress{{Rule: RuleLatency, Bad: 9}}
				if err := s.flush(t.Context(), replacement); err != nil {
					t.Fatal(err)
				}
				if rollback && expire {
					if got := countTelemetry(t, assertions, schema); got != (telemetryCounts{}) {
						t.Fatalf("expired absent receipt was replayed: %+v", got)
					}
					if !s.applied.IsZero() || len(s.progress.Rules) != 0 || c.dropped != 2 {
						t.Fatal("expired absent batch changed progress or lost its accounting")
					}
				} else {
					wantProgress := original
					if rollback {
						wantProgress = replacement
					}
					if !reflect.DeepEqual(s.progress.Rules, wantProgress) || s.applied != b.id {
						t.Fatal("reconciled progress differs from committed receipt")
					}
					if got := countTelemetry(t, assertions, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: 2, issues: 1, events: 1}) {
						t.Fatalf("retry duplicated or omitted work: %+v", got)
					}
					if row := storedIssue(t, assertions, schema, fingerprint(b.issues[0])); row.Count != 1 {
						t.Fatal("same receipt incremented issue twice")
					}
					if !reflect.DeepEqual(storedReceipt(t, assertions, schema, b).Rules, wantProgress) {
						t.Fatal("applied progress is not receipt progress")
					}
				}
				if afterCommit.Load() != 0 {
					t.Fatal("reconciliation depended on rerunning a missing callback")
				}
				wantPending := 0
				if expire {
					wantPending = 1
				}
				if len(c.pending) != wantPending {
					t.Fatal("reconciliation consumed a later FIFO batch")
				}
			})
		}
	}
}

func TestPostgresTelemetryCommittedAfterCommitErrorStillAppliesOnce(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	c := s.collector
	c.ObserveRequest(t.Context(), request())
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	b := capture(t, c)
	writer := s.transact
	callbackFailure := errors.New("injected private after-commit failure")
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return writer(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			return tx.AfterCommit(func(context.Context) error { return callbackFailure })
		}, options...)
	}
	progress := []ruleProgress{{Rule: RuleLatency, Bad: 2}}
	if err := s.flush(t.Context(), progress); !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, callbackFailure) {
		t.Fatalf("committed completion failure = %v", err)
	}
	if len(c.pending) != 0 || s.applied != b.id || !reflect.DeepEqual(s.progress.Rules, progress) {
		t.Fatal("confirmed commit was not applied after callback failure")
	}
	if c.failures != 1 {
		t.Fatal("completion failure was not counted separately")
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: 2, issues: 1, events: 1}) {
		t.Fatalf("committed rows = %+v", got)
	}
	if err := s.flush(t.Context(), []ruleProgress{{Rule: RuleLatency, Bad: 3}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.progress.Rules, progress) || storedIssue(t, db, schema, fingerprint(b.issues[0])).Count != 1 {
		t.Fatal("empty queue repeated a committed batch")
	}
}
