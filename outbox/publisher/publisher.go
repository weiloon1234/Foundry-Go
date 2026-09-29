// Package publisher transfers shared outbox messages to durable authorities.
// One short transaction locks a batch of due rows with SKIP LOCKED across every
// route, publishes their stable identities with bounded concurrency, then records
// each outcome. A crash/unknown commit may publish again; destination
// deduplication retention must cover this recovery window.
package publisher

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type MessageID = model.ID[Publication]
type Publication struct{}

// Message is the explicit heterogeneous publication boundary. Feature adapters
// validate their own payload schema again. Default formatting omits private data.
type Message struct{ record outboxstore.Message }

func (m Message) ID() MessageID                       { return model.IDFromBytes[Publication](m.record.ID.Bytes()) }
func (m Message) Name() string                        { return m.record.Name }
func (m Message) Version() uint32                     { return m.record.Version }
func (m Message) Destination() outbox.Destination     { return m.record.Destination }
func (m Message) PayloadJSON() (string, error)        { return m.record.Payload.Text() }
func (m Message) Origin() (attribution.Origin, error) { return m.record.Origin.Decode() }
func (Message) Format(s fmt.State, _ rune)            { _, _ = s.Write([]byte("outbox publication")) }

// Route must return nil only after durable acceptance, including an unchanged
// duplicate of this stable ID. It must honor cancellation and never log payloads.
// Failed attempts retain the row. No network operation is implicitly replayed.
type Route struct {
	Kind        string
	Destination outbox.Destination
	Publish     func(context.Context, Message) error
}

// Config bounds publication. A failed attempt retries after RetryDelay doubled
// per attempt up to MaxRetryDelay (zero keeps a fixed RetryDelay), plus additive
// jitter in [0, max(Jitter, delay/5)] (zero Jitter disables it). The retry budget
// ends after MaxAttempts attempts: the defaults (1s doubling to 5 minutes, 1000
// attempts) keep retrying through roughly 83 hours of broker outage.
//
// MaxInFlight bounds both the rows claimed per transaction and concurrent
// publications. Idle polling starts at PollInterval and doubles up to
// MaxPollInterval (zero keeps a fixed interval). Logger receives redacted
// failure diagnostics; Module supplies the application logger when it is nil.
type Config struct {
	MaxAttempts      uint32
	RetryDelay       time.Duration
	MaxRetryDelay    time.Duration
	Jitter           time.Duration
	OperationTimeout time.Duration
	PollInterval     time.Duration
	MaxPollInterval  time.Duration
	MaxInFlight      int
	Clock            clock.Clock
	Logger           *slog.Logger
	// Observe receives actual delivery errors in process, after SQL commit. It
	// runs as an owned callback; its failure is logged and never stops Run. No
	// errors enter row payloads.
	Observe func(context.Context, Result) error
}

func DefaultConfig() Config {
	return Config{MaxAttempts: 1000, RetryDelay: time.Second, MaxRetryDelay: 5 * time.Minute, Jitter: time.Second, OperationTimeout: 10 * time.Second, PollInterval: 100 * time.Millisecond, MaxPollInterval: time.Second, MaxInFlight: 16, Clock: clock.System{}}
}
func (c Config) Validate() error {
	if c.MaxAttempts == 0 || c.MaxAttempts > 1000000 || c.RetryDelay <= 0 || c.RetryDelay > 24*time.Hour || c.OperationTimeout <= 0 || c.OperationTimeout > time.Hour || c.PollInterval <= 0 || c.PollInterval > time.Minute || c.MaxInFlight < 1 || c.MaxInFlight > 1024 || nilValue(c.Clock) {
		return fault.New(fault.Invalid, "invalid outbox publication bounds or clock")
	}
	if c.MaxRetryDelay < 0 || c.MaxRetryDelay > 24*time.Hour || c.MaxRetryDelay != 0 && c.MaxRetryDelay < c.RetryDelay || c.Jitter < 0 || c.Jitter > time.Hour || c.MaxPollInterval < 0 || c.MaxPollInterval > time.Minute || c.MaxPollInterval != 0 && c.MaxPollInterval < c.PollInterval {
		return fault.New(fault.Invalid, "invalid outbox retry or polling bounds")
	}
	return nil
}
func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// retryDelay is the exponential, jittered wait after a failed attempt number.
func (c Config) retryDelay(attempts uint32) time.Duration {
	delay := c.RetryDelay
	ceiling := max(c.MaxRetryDelay, c.RetryDelay)
	for i := uint32(1); i < attempts && delay < ceiling; i++ {
		delay *= 2
	}
	delay = min(delay, ceiling)
	if c.Jitter > 0 {
		delay += rand.N(max(c.Jitter, delay/5) + 1)
	}
	return delay
}

// Result records one committed publication decision. Published means accepted
// by its delivery authority, not handler success. Failure is private in-process
// diagnostic data and is deliberately omitted by default formatting.
type Result struct {
	Committed bool
	ID        MessageID
	Found     bool
	State     outbox.PublicationState
	Attempts  uint32
	Failure   error
}

func (Result) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbox publication result")) }

type routeKey struct {
	kind        string
	destination outbox.Destination
}
type Publisher struct {
	writer  database.Transactor
	config  Config
	routes  map[routeKey]Route
	claim   query.Predicate[outboxstore.Message]
	slots   *admission.Semaphore
	running atomic.Bool
}

// New borrows a transaction authority that commits each callback as an outer
// transaction. Passing a scoped *database.Tx is rejected. Wrappers must preserve
// that ownership contract (for example selecting an isolated schema per call).
// Construction performs no I/O or migrations. Routes freeze before publication.
func New(writer database.Transactor, config Config, routes ...Route) (*Publisher, error) {
	if nilValue(writer) {
		return nil, fault.New(fault.Invalid, "outbox publisher requires a transaction authority")
	}
	if _, nested := writer.(*database.Tx); nested {
		return nil, fault.New(fault.Invalid, "outbox publisher cannot borrow a business transaction")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if len(routes) == 0 || len(routes) > 128 {
		return nil, fault.New(fault.Invalid, "outbox publisher requires 1 to 128 routes")
	}
	fields := outboxstore.MessageFields()
	index := make(map[routeKey]Route, len(routes))
	claims := make([]query.Predicate[outboxstore.Message], 0, len(routes))
	for _, route := range routes {
		if !identifier.Semantic(route.Kind) || route.Publish == nil {
			return nil, fault.New(fault.Invalid, "invalid outbox publication route")
		}
		if err := route.Destination.Validate(); err != nil {
			return nil, err
		}
		key := routeKey{route.Kind, route.Destination}
		if _, seen := index[key]; seen {
			return nil, fault.New(fault.Duplicate, "outbox publication route already registered")
		}
		index[key] = route
		claims = append(claims, query.And(fields.Kind.Eq(route.Kind), fields.Destination.Eq(route.Destination)))
	}
	return &Publisher{writer: writer, config: config, routes: index, claim: query.Or(claims...), slots: admission.New(config.MaxInFlight)}, nil
}

// PublishOne publishes at most one due row across all routes.
func (p *Publisher) PublishOne(ctx context.Context) (Result, error) {
	if err := p.check(ctx); err != nil {
		return Result{}, err
	}
	results, err := p.publish(ctx, 1, p.config.Logger)
	if len(results) == 0 {
		return Result{}, err
	}
	return results[0], err
}

// PublishBatch claims up to MaxInFlight due rows across all routes in one
// transaction and publishes them concurrently. Publication order within a batch
// is not preserved. Every callback is retained until actual exit. SQL
// cancellation or connection loss can release row locks before that exit;
// destination deduplication must therefore also cover overlapping publications.
func (p *Publisher) PublishBatch(ctx context.Context) ([]Result, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	return p.publish(ctx, p.config.MaxInFlight, p.config.Logger)
}

func (p *Publisher) check(ctx context.Context) error {
	if p == nil || p.slots == nil || ctx == nil {
		return fault.New(fault.Invalid, "outbox publisher requires initialization and context")
	}
	return nil
}

func (p *Publisher) publish(ctx context.Context, limit int, logger *slog.Logger) ([]Result, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Claim only as many rows as publication slots are held, so concurrent
	// callers never lock rows they cannot publish.
	if err := p.slots.Acquire(ctx, admission.Wait(p.config.OperationTimeout), nil); err != nil {
		return nil, err
	}
	held := 1
	for held < limit && p.slots.TryAcquire() {
		held++
	}
	defer func() {
		for range held {
			p.slots.Release()
		}
	}()
	// The transaction outlives the publication budget by a bookkeeping margin,
	// so a publish that uses its whole deadline still records every claimed
	// row's outcome (including its own counted, timed-out attempt) instead of
	// rolling back the batch and republishing its successful rows. Caller
	// cancellation still rolls the whole batch back.
	margin := p.bookkeeping()
	transaction, cancel := context.WithTimeout(ctx, p.config.OperationTimeout+margin)
	defer cancel()
	// SQL instants have microsecond precision. Rounding eligibility down must
	// never select a row before its persisted publication deadline.
	now, err := temporal.NewDateTime(p.config.Clock.Now().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	fields := outboxstore.MessageFields()
	var results []Result
	err = p.writer.Transaction(transaction, func(tx *database.Tx) error {
		rows, err := outboxstore.QueryFoundryOutbox().Where(p.claim, fields.PublishState.Eq(outbox.Pending), fields.PublishAfter.Lte(now)).OrderBy(fields.CreatedAt.Asc(), fields.ID.Asc()).ForUpdate().SkipLocked().Limit(held).All(transaction, tx)
		if err != nil {
			return err
		}
		results = make([]Result, len(rows))
		// Publications share one budget ending a margin before the transaction
		// deadline, leaving time for the outcome updates and commit.
		deadline := time.Now().Add(p.config.OperationTimeout)
		if limit, ok := transaction.Deadline(); ok && deadline.After(limit.Add(-margin)) {
			deadline = limit.Add(-margin)
		}
		publishing, stop := context.WithDeadline(transaction, deadline)
		defer stop()
		var group sync.WaitGroup
		for i, row := range rows {
			results[i] = Result{ID: model.IDFromBytes[Publication](row.ID.Bytes()), Found: true, Attempts: row.PublishAttempts}
			route, ok := p.routes[routeKey{row.Kind, row.Destination}]
			if !ok || row.PublishAttempts >= p.config.MaxAttempts {
				continue
			}
			results[i].Attempts++
			group.Go(func() {
				results[i].Failure = callback.Isolated("publish outbox message", func() error { return route.Publish(publishing, Message{record: row}) })
			})
		}
		group.Wait()
		for i, row := range rows {
			draft, err := p.outcome(&results[i], row.PublishAttempts == results[i].Attempts)
			if err != nil {
				return err
			}
			if _, err := outboxstore.QueryFoundryOutbox().Update(transaction, tx, row.ID, draft); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return results, err
	}
	for i := range results {
		results[i].Committed = true
	}
	if p.config.Observe != nil {
		for _, result := range results {
			if err := callback.Isolated("observe outbox publication", func() error { return p.config.Observe(ctx, result) }); err != nil {
				p.log(context.WithoutCancel(ctx), logger, slog.LevelWarn, "outbox publication observer failed", err)
			}
		}
	}
	return results, nil
}

// bookkeeping is the time the batch transaction keeps after the publication
// budget to record outcomes and commit.
func (p *Publisher) bookkeeping() time.Duration {
	return min(max(p.config.OperationTimeout/4, time.Second), 30*time.Second)
}

// outcome classifies one attempt and derives its progress update. skipped means
// the row's budget was already exhausted (for example after lowering it).
func (p *Publisher) outcome(result *Result, skipped bool) (outboxstore.MessageDraft, error) {
	finishedAt := p.config.Clock.Now()
	permanent := false
	switch {
	case skipped:
		result.State = outbox.PublicationFailed
	case result.Failure == nil:
		result.State = outbox.Published
	default:
		permanent = permanentFailure(result.Failure)
		if permanent || result.Attempts >= p.config.MaxAttempts {
			result.State = outbox.PublicationFailed
		} else {
			result.State = outbox.Pending
		}
	}
	draft := outboxstore.MessageDraft{}.SetPublishState(result.State).SetPublishAttempts(result.Attempts)
	switch result.State {
	case outbox.Published:
		finished, err := temporal.NewDateTime(finishedAt.Truncate(time.Microsecond))
		if err != nil {
			return draft, err
		}
		return draft.SetPublishedAt(finished).SetPublishReason(""), nil
	case outbox.Pending:
		// Add the delay to the original sample, then round the deadline up.
		// Truncating either operand could publish a retry early.
		deadline := finishedAt.Add(p.config.retryDelay(result.Attempts))
		if remainder := deadline.Nanosecond() % int(time.Microsecond); remainder != 0 {
			deadline = deadline.Add(time.Microsecond - time.Duration(remainder))
		}
		next, err := temporal.NewDateTime(deadline)
		if err != nil {
			return draft, err
		}
		return draft.SetPublishAfter(next).SetPublishReason("transient"), nil
	}
	reason := "attempt_limit"
	if permanent {
		reason = "permanent"
	}
	return draft.SetPublishReason(reason), nil
}

// Run publishes until ctx ends. Transient SQL, transaction and publication
// failures are logged with redacted diagnostics and retried with jittered
// exponential backoff; Run returns only on cancellation or invalid use.
func (p *Publisher) Run(ctx context.Context) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	return p.run(ctx, p.config.Logger)
}

func (p *Publisher) run(ctx context.Context, logger *slog.Logger) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	if !p.running.CompareAndSwap(false, true) {
		return fault.New(fault.Conflict, "outbox publisher is already running")
	}
	defer p.running.Store(false)
	idle, failures := p.config.PollInterval, 0
	for ctx.Err() == nil {
		results, err := p.publish(ctx, p.config.MaxInFlight, logger)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			failures++
			p.log(ctx, logger, slog.LevelError, "outbox publication batch failed", err)
			if !pause(ctx, p.failureDelay(failures)) {
				return nil
			}
			continue
		}
		failures = 0
		published := 0
		for _, result := range results {
			switch {
			case result.State == outbox.Published:
				published++
			case result.Failure != nil && result.State == outbox.Pending:
				p.log(ctx, logger, slog.LevelWarn, "outbox publication attempt failed", result.Failure, slog.String("message_id", result.ID.String()), slog.Uint64("attempts", uint64(result.Attempts)), slog.String("state", string(result.State)))
			case result.State == outbox.PublicationFailed:
				p.log(ctx, logger, slog.LevelError, "outbox publication failed permanently", result.Failure, slog.String("message_id", result.ID.String()), slog.Uint64("attempts", uint64(result.Attempts)), slog.String("state", string(result.State)))
			}
		}
		switch {
		case len(results) == 0:
			if !pause(ctx, idle) {
				return nil
			}
			if p.config.MaxPollInterval > p.config.PollInterval {
				idle = min(idle*2, p.config.MaxPollInterval)
			}
		case published < len(results):
			// Failed rows now carry future deadlines; pause briefly so a broker
			// outage does not turn remaining due rows into a hot loop.
			idle = p.config.PollInterval
			if !pause(ctx, p.config.PollInterval) {
				return nil
			}
		default:
			idle = p.config.PollInterval
		}
	}
	return nil
}

// failureDelay backs off after whole-batch failures (for example an unavailable
// database), from PollInterval up to MaxRetryDelay, selecting from the upper half.
func (p *Publisher) failureDelay(failures int) time.Duration {
	ceiling := max(p.config.MaxRetryDelay, p.config.RetryDelay, p.config.PollInterval)
	delay := p.config.PollInterval
	for i := 1; i < failures && delay < ceiling; i++ {
		delay *= 2
	}
	delay = min(delay, ceiling)
	half := delay / 2
	return half + rand.N(delay-half+1)
}

func (p *Publisher) log(ctx context.Context, logger *slog.Logger, level slog.Level, message string, err error, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	attrs = append(slices.Clone(attrs), slog.Any("diagnostic", errordiag.Describe(err)))
	_ = callback.Isolated("log outbox publication", func() error {
		logger.LogAttrs(ctx, level, message, attrs...)
		return nil
	})
}

func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
