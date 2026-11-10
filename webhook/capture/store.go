package capture

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
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
)

// Dependencies are borrowed; the Store never closes them.
type Dependencies struct {
	DB *database.DB
	// Schema holds foundry_webhook_captures; it must be a valid identifier.
	Schema string
	// Clock supplies retention cutoffs. Receive times come from the kernel.
	Clock  clock.Clock
	Logger *slog.Logger
}

// Store owns the capture table: the single writer that drains the recorder,
// retention, and the bounded reads behind Routes. Applications hold it to
// mount Routes; Module drives its lifecycle. Construct it with New.
type Store struct {
	recorder *httpcapture.Recorder
	scope    store.Scope
	schema   string
	config   Config
	policy   policy
	clock    clock.Clock
	logger   *slog.Logger
	reads    chan struct{}
	done     chan struct{}
	closing  sync.Once

	// mu serializes writing between Run and Close and guards the fields below.
	mu        sync.Mutex
	available bool
	warned    bool
	pending   *pendingRecord
	reported  [httpcapture.DropReasons]uint64
}

type pendingRecord struct {
	record *httpcapture.Record
	row    store.Capture
}

// New validates its inputs without I/O. A nil recorder gives a store that
// reads and prunes but never writes, as when capture is disabled at runtime.
//
// Example: capture.New(recorder, capture.Dependencies{DB: db, Schema: "public", Clock: clock.System{}, Logger: logger}, capture.DefaultConfig())
func New(recorder *httpcapture.Recorder, deps Dependencies, config Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if deps.Clock == nil || deps.Logger == nil {
		return nil, invalid("webhook capture store requires a clock and logger")
	}
	if recorder != nil && recorder.BodyBytes() != config.BodyBytes {
		return nil, invalid("webhook capture recorder and store limits differ")
	}
	scope, err := store.NewScope(deps.DB, deps.Schema)
	if err != nil {
		return nil, err
	}
	return &Store{recorder: recorder, scope: scope, schema: deps.Schema, config: config, policy: config.policy(), clock: deps.Clock, logger: deps.Logger, reads: make(chan struct{}, maxReads), done: make(chan struct{})}, nil
}

// Format writes a fixed label without exposing dependencies or captures.
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook capture store")) }

// Run writes captures and prunes the table until ctx ends. Database trouble
// never stops it: an unavailable table degrades capture with one logged
// reason, failed batches are retried twice and then counted as lost.
func (s *Store) Run(ctx context.Context) error {
	s.check(ctx)
	prune := time.NewTimer(0)
	defer prune.Stop()
	recheck := time.NewTicker(recheckInterval)
	defer recheck.Stop()
	loss := time.NewTicker(lossLogInterval)
	defer loss.Stop()
	var ready <-chan struct{}
	if s.recorder != nil {
		ready = s.recorder.Ready()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ready:
			s.flush(ctx, context.WithoutCancel(ctx))
		case <-prune.C:
			next := pruneInterval
			if behind := s.prunePass(ctx); behind {
				next = pruneCatchUp
			}
			prune.Reset(next)
		case <-recheck.C:
			s.check(ctx)
		case <-loss.C:
			s.reportLoss()
		}
	}
}

// Close stops admission, writes what remains within a short drain budget and
// counts the rest as lost. It runs after the HTTP kernel has drained.
func (s *Store) Close(ctx context.Context) error {
	s.closing.Do(func() {
		defer close(s.done)
		if s.recorder == nil {
			return
		}
		s.recorder.Seal()
		drain, cancel := context.WithTimeout(ctx, drainTimeout)
		defer cancel()
		s.flush(drain, drain)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending != nil {
			s.lose(httpcapture.DropShutdown, *s.pending)
			s.pending = nil
		}
		for record := s.recorder.Drain(); record != nil; record = s.recorder.Drain() {
			s.lose(httpcapture.DropShutdown, pendingRecord{record: record})
		}
		s.reportLossLocked()
	})
	return nil
}

// Done closes once Close has finished.
func (s *Store) Done() <-chan struct{} { return s.done }

func (s *Store) check(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.available {
		return
	}
	checked, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	present, err := s.scope.Exists(checked)
	s.available = err == nil && present
	switch {
	case s.available && s.warned:
		s.warned = false
		s.logger.Info("webhook capture table is available; capture resumed", "schema", s.schema)
	case !s.available && !s.warned:
		s.warned = true
		reason := "missing"
		if err != nil {
			reason = "unreachable"
		}
		s.logger.Warn("webhook capture table is unavailable; captures are dropped until it is migrated and reachable", "schema", s.schema, "reason", reason)
	}
}

// flush writes queued captures in bounded batches, one transaction at a time,
// until the queue is empty or stop ends. Writes use parent, so shutdown does
// not abort a batch already in flight. Each batch stays charged to the
// recorder until it is stored or lost.
func (s *Store) flush(stop, parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for stop.Err() == nil {
		batch := s.take()
		if len(batch) == 0 {
			return
		}
		s.write(parent, batch)
	}
}

func (s *Store) take() []pendingRecord {
	var batch []pendingRecord
	size := 0
	for len(batch) < batchRows {
		next := s.pending
		s.pending = nil
		if next == nil {
			record := s.recorder.Drain()
			if record == nil {
				break
			}
			row, err := redact(record.Snapshot, s.policy)
			if err != nil {
				s.lose(httpcapture.DropWrite, pendingRecord{record: record})
				continue
			}
			next = &pendingRecord{record: record, row: row}
		}
		if len(batch) > 0 && size+next.row.Size() > batchBytes {
			s.pending = next
			break
		}
		batch = append(batch, *next)
		size += next.row.Size()
	}
	return batch
}

func (s *Store) write(ctx context.Context, batch []pendingRecord) {
	if !s.available {
		for _, item := range batch {
			s.lose(httpcapture.DropWrite, item)
		}
		return
	}
	rows := make([]store.Capture, len(batch))
	for i, item := range batch {
		rows[i] = item.row
	}
	backoff := retryBackoff
	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, writeTimeout)
		err := s.scope.Transaction(attemptCtx, false, func(tx *database.Tx) error { return store.Insert(attemptCtx, tx, rows) })
		cancel()
		if err == nil {
			for _, item := range batch {
				item.record.Release()
			}
			return
		}
		if attempt == writeRetries || !wait(ctx, backoff) {
			for _, item := range batch {
				s.lose(httpcapture.DropWrite, item)
			}
			// Stop writing until the periodic check finds the table again.
			s.available = false
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

// lose counts one record and the loss it carried, then releases its memory.
func (s *Store) lose(reason httpcapture.DropReason, item pendingRecord) {
	s.recorder.Lost(reason, 1, item.record.Snapshot.DroppedBefore)
	item.record.Release()
}

func (s *Store) reportLoss() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reportLossLocked()
}

// reportLossLocked logs counts only, never request data or error text.
func (s *Store) reportLossLocked() {
	if s.recorder == nil {
		return
	}
	drops := s.recorder.Drops()
	if drops == s.reported {
		return
	}
	delta := func(reason httpcapture.DropReason) uint64 { return drops[reason] - s.reported[reason] }
	s.logger.Warn("webhook captures were not stored",
		"rate", delta(httpcapture.DropRate), "capacity", delta(httpcapture.DropCapacity), "memory", delta(httpcapture.DropBytes),
		"write", delta(httpcapture.DropWrite), "shutdown", delta(httpcapture.DropShutdown), "sealed", delta(httpcapture.DropSealed))
	s.reported = drops
}
