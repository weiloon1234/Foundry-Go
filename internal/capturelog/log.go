// Package capturelog is the shared persistence engine for append-only HTTP
// capture tables: the single writer per process that drains a recorder,
// batching, retries, availability checks, loss logging, retention by age and
// row cap under an advisory lock, the bounded viewer read gate and the prune
// command. It knows nothing about HTTP disclosure; each table supplies its
// typed declaration and a redact function.
package capturelog

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const (
	batchRows       = 32
	batchBytes      = 1 << 20
	writeTimeout    = 500 * time.Millisecond
	writeRetries    = 2
	retryBackoff    = 100 * time.Millisecond
	drainTimeout    = 3 * time.Second
	checkTimeout    = 2 * time.Second
	recheckInterval = 30 * time.Second
	lossLogInterval = time.Minute
	maxReads        = 2
	readTimeout     = 2 * time.Second
)

// Table describes one capture table through its handwritten declaration.
type Table[M any] struct {
	// Name is the table name used by the availability check.
	Name string
	// Label names the rows in logs and command output, e.g. "webhook capture".
	Label string
	// Lock is the advisory-lock namespace retention holds per schema.
	Lock  string
	Query func() orm.Query[M]
	ID    orm.Key[M, model.ID[M]]
	At    orm.Time[M, temporal.DateTime]
	// Partition, when set, applies the row cap per distinct value of this
	// column instead of to the whole table.
	Partition *orm.Text[M, string]
	Insert    func(context.Context, *database.Tx, []M) error
	// Size estimates a row's payload for write batching.
	Size func(M) int
}

// Dependencies are borrowed; the log never closes them.
type Dependencies struct {
	DB *database.DB
	// Schema holds the table; it must be a valid identifier.
	Schema string
	// Clock supplies retention cutoffs. Capture times come from the producer.
	Clock  clock.Clock
	Logger *slog.Logger
}

// Limits bound the table. Each public capture config validates its own range.
type Limits struct {
	Retention time.Duration
	MaxRows   int
}

// Engine is a log's operations without its row and snapshot types. Public
// capture stores hold it through this interface, so packages that only name a
// store never instantiate the generic engine and its table declaration.
type Engine interface {
	Pruner
	Run(ctx context.Context, roles Roles) error
	Close(ctx context.Context) error
	Done() <-chan struct{}
	Check(ctx context.Context)
	Flush(ctx context.Context)
	PrunePass(ctx context.Context) bool
	Read(ctx context.Context, fn func(context.Context, *database.Tx) error) error
	Transaction(ctx context.Context, readOnly bool, fn func(*database.Tx) error) error
	Lock(ctx context.Context, tx *database.Tx) (bool, error)
}

// Redact turns one drained snapshot and the loss its lane carried into a row.
type Redact[S, M any] func(snapshot S, droppedBefore uint64) (M, error)

// Log is one table's writer, retention and reads. A nil recorder gives a log
// that reads and prunes but never writes.
type Log[S, M any] struct {
	table    Table[M]
	recorder *httpcapture.Recorder[S]
	redact   Redact[S, M]
	db       *database.DB
	schema   string
	limits   Limits
	clock    clock.Clock
	logger   *slog.Logger
	reads    chan struct{}
	done     chan struct{}
	closing  sync.Once

	// mu serializes writing between Run and Close and guards the fields below.
	mu        sync.Mutex
	checked   bool
	available bool
	warned    bool
	pending   *pending[S, M]
	reported  [httpcapture.DropReasons]uint64
}

type pending[S, M any] struct {
	record *httpcapture.Record[S]
	row    M
}

// New validates its inputs without I/O.
func New[S, M any](table Table[M], recorder *httpcapture.Recorder[S], redact Redact[S, M], deps Dependencies, limits Limits) (*Log[S, M], error) {
	if table.Query == nil || table.Insert == nil || table.Size == nil || !sqlname.Valid(table.Name) || table.Label == "" || table.Lock == "" ||
		deps.DB == nil || !sqlname.Valid(deps.Schema) || deps.Clock == nil || deps.Logger == nil || recorder != nil && redact == nil || limits.MaxRows < 1 || limits.Retention <= 0 {
		return nil, safeerror.NewFault(fault.Invalid, "capture log requires a table, database, valid schema, clock, logger and limits")
	}
	return &Log[S, M]{table: table, recorder: recorder, redact: redact, db: deps.DB, schema: deps.Schema, limits: limits, clock: deps.Clock, logger: deps.Logger, reads: make(chan struct{}, maxReads), done: make(chan struct{})}, nil
}

// Format writes a fixed label without exposing dependencies or captures.
func (*Log[S, M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("capture log")) }

// Label names the table's rows in logs and command output.
func (l *Log[S, M]) Label() string { return l.table.Label }

// Roles chooses what Run does in this process.
type Roles struct {
	// Write drains the recorder. The table is checked on the first record.
	Write bool
	// Retain prunes on a timer; its first pass also checks the table.
	Retain bool
}

// Run writes captures and prunes the table until ctx ends. Database trouble
// never stops it: an unavailable table degrades capture with one logged
// reason, failed batches are retried twice and then counted as lost.
func (l *Log[S, M]) Run(ctx context.Context, roles Roles) error {
	recheck := time.NewTicker(recheckInterval)
	defer recheck.Stop()
	loss := time.NewTicker(lossLogInterval)
	defer loss.Stop()
	var ready <-chan struct{}
	if roles.Write && l.recorder != nil {
		ready = l.recorder.Ready()
	}
	var prune *time.Timer
	var pruneDue <-chan time.Time
	if roles.Retain {
		prune = time.NewTimer(0)
		defer prune.Stop()
		pruneDue = prune.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ready:
			l.flush(ctx, context.WithoutCancel(ctx))
		case <-pruneDue:
			next := pruneInterval
			if behind := l.PrunePass(ctx); behind {
				next = pruneCatchUp
			}
			prune.Reset(next)
		case <-recheck.C:
			l.recheck(ctx)
		case <-loss.C:
			l.reportLoss()
		}
	}
}

// Close stops admission, writes what remains within a short drain budget and
// counts the rest as lost. It runs after every producer has drained.
func (l *Log[S, M]) Close(ctx context.Context) error {
	l.closing.Do(func() {
		defer close(l.done)
		if l.recorder == nil {
			return
		}
		l.recorder.Seal()
		drain, cancel := context.WithTimeout(ctx, drainTimeout)
		defer cancel()
		l.flush(drain, drain)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.pending != nil {
			l.pending.record.Lose(httpcapture.DropShutdown)
			l.pending = nil
		}
		for record := l.recorder.Drain(); record != nil; record = l.recorder.Drain() {
			record.Lose(httpcapture.DropShutdown)
		}
		l.reportLossLocked()
	})
	return nil
}

// Done closes once Close has finished.
func (l *Log[S, M]) Done() <-chan struct{} { return l.done }

// Check queries the table's availability now and logs a change.
func (l *Log[S, M]) Check(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checkLocked(ctx)
}

func (l *Log[S, M]) recheck(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.checked && !l.available {
		l.checkLocked(ctx)
	}
}

func (l *Log[S, M]) checkLocked(ctx context.Context) {
	checked, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	var present bool
	err := database.ScanOne(checked, l.db.Primary(), `SELECT pg_catalog.to_regclass(pg_catalog.quote_ident($1) || '.' || pg_catalog.quote_ident($2)) IS NOT NULL`, []any{l.schema, l.table.Name}, &present)
	if err != nil && ctx.Err() != nil {
		// The caller stopped; the table's state is still unknown.
		return
	}
	l.checked = true
	l.available = err == nil && present
	switch {
	case l.available && l.warned:
		l.warned = false
		l.logger.Info(l.table.Label+" table is available; capture resumed", "schema", l.schema)
	case !l.available && !l.warned:
		l.warned = true
		reason := "missing"
		if err != nil {
			reason = "unreachable"
		}
		l.logger.Warn(l.table.Label+" table is unavailable; captures are dropped until it is migrated and reachable", "schema", l.schema, "reason", reason)
	}
}

// Flush writes every queued capture now, checking the table first when it
// has not been checked yet.
func (l *Log[S, M]) Flush(ctx context.Context) { l.flush(ctx, ctx) }

// flush writes queued captures in bounded batches, one transaction at a time,
// until the queue is empty or stop ends. Writes use parent, so shutdown does
// not abort a batch already in flight. Each batch stays charged to the
// recorder until it is stored or lost.
func (l *Log[S, M]) flush(stop, parent context.Context) {
	if l.recorder == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for stop.Err() == nil {
		batch := l.take()
		if len(batch) == 0 {
			return
		}
		if !l.checked {
			l.checkLocked(parent)
		}
		l.write(parent, batch)
	}
}

func (l *Log[S, M]) take() []pending[S, M] {
	var batch []pending[S, M]
	size := 0
	for len(batch) < batchRows {
		next := l.pending
		l.pending = nil
		if next == nil {
			record := l.recorder.Drain()
			if record == nil {
				break
			}
			row, err := l.redact(record.Snapshot, record.DroppedBefore)
			if err != nil {
				record.Lose(httpcapture.DropWrite)
				continue
			}
			next = &pending[S, M]{record: record, row: row}
		}
		if len(batch) > 0 && size+l.table.Size(next.row) > batchBytes {
			l.pending = next
			break
		}
		batch = append(batch, *next)
		size += l.table.Size(next.row)
	}
	return batch
}

func (l *Log[S, M]) write(ctx context.Context, batch []pending[S, M]) {
	if !l.available {
		for _, item := range batch {
			item.record.Lose(httpcapture.DropWrite)
		}
		return
	}
	rows := make([]M, len(batch))
	for i, item := range batch {
		rows[i] = item.row
	}
	backoff := retryBackoff
	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, writeTimeout)
		err := l.Transaction(attemptCtx, false, func(tx *database.Tx) error { return l.table.Insert(attemptCtx, tx, rows) })
		cancel()
		if err == nil {
			for _, item := range batch {
				item.record.Release()
			}
			return
		}
		if attempt == writeRetries || !wait(ctx, backoff) {
			for _, item := range batch {
				item.record.Lose(httpcapture.DropWrite)
			}
			// Stop writing until the periodic check finds the table again.
			l.available = false
			return
		}
		backoff *= 2
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *Log[S, M]) reportLoss() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reportLossLocked()
}

// reportLossLocked logs counts only, never captured data or error text.
func (l *Log[S, M]) reportLossLocked() {
	if l.recorder == nil {
		return
	}
	drops := l.recorder.Drops()
	if drops == l.reported {
		return
	}
	delta := func(reason httpcapture.DropReason) uint64 { return drops[reason] - l.reported[reason] }
	l.logger.Warn(l.table.Label+"s were not stored",
		"rate", delta(httpcapture.DropRate), "capacity", delta(httpcapture.DropCapacity), "memory", delta(httpcapture.DropBytes),
		"write", delta(httpcapture.DropWrite), "shutdown", delta(httpcapture.DropShutdown), "sealed", delta(httpcapture.DropSealed))
	l.reported = drops
}

// Transaction scopes fn to the capture schema inside one owned transaction on
// the borrowed primary pool. Reads use the primary too: recent captures may
// not have reached a replica yet.
func (l *Log[S, M]) Transaction(ctx context.Context, readOnly bool, fn func(*database.Tx) error) error {
	options := database.TxOptions{Isolation: database.ReadCommitted, ReadOnly: readOnly}
	return l.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+l.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, options)
}

// Read admits at most two concurrent viewer reads, each bounded by two
// seconds including the wait for a slot, in a read-only transaction.
func (l *Log[S, M]) Read(ctx context.Context, fn func(context.Context, *database.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	select {
	case l.reads <- struct{}{}:
	case <-ctx.Done():
		return http.Unavailable.WithCause(ctx.Err())
	}
	defer func() { <-l.reads }()
	return l.Transaction(ctx, true, func(tx *database.Tx) error { return fn(ctx, tx) })
}

// Now is the log clock's current instant.
func (l *Log[S, M]) Now() (temporal.DateTime, error) { return temporal.NewDateTime(l.clock.Now()) }

// Cutoff is now minus retention; reads hide older rows before pruning does.
func (l *Log[S, M]) Cutoff() (temporal.DateTime, error) {
	return temporal.NewDateTime(l.clock.Now().Add(-l.limits.Retention))
}

// DroppedBefore clamps a loss count into its signed column.
func DroppedBefore(count uint64) int64 { return int64(min(count, math.MaxInt64)) }
