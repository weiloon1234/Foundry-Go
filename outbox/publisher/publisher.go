// Package publisher transfers shared outbox messages to durable authorities.
// One short transaction locks a pending row with SKIP LOCKED, publishes its
// stable identity, then records acceptance. A crash/unknown commit may publish
// again; destination deduplication retention must cover this recovery window.
package publisher

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
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
type Config struct {
	MaxAttempts      uint32
	RetryDelay       time.Duration
	OperationTimeout time.Duration
	PollInterval     time.Duration
	MaxInFlight      int
	Clock            clock.Clock
	// Observe receives actual delivery errors in process, after SQL commit. It
	// runs as an owned callback; its error stops Run. No errors enter row payloads.
	Observe func(context.Context, Result) error
}

func DefaultConfig() Config {
	return Config{MaxAttempts: 100, RetryDelay: time.Second, OperationTimeout: 10 * time.Second, PollInterval: 100 * time.Millisecond, MaxInFlight: 8, Clock: clock.System{}}
}
func (c Config) Validate() error {
	if c.MaxAttempts == 0 || c.MaxAttempts > 1000000 || c.RetryDelay <= 0 || c.RetryDelay > 24*time.Hour || c.OperationTimeout <= 0 || c.OperationTimeout > time.Hour || c.PollInterval <= 0 || c.PollInterval > time.Minute || c.MaxInFlight < 1 || c.MaxInFlight > 1024 || nilValue(c.Clock) {
		return fault.New(fault.Invalid, "invalid outbox publication bounds or clock")
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

type Publisher struct {
	writer  database.Transactor
	config  Config
	routes  []Route
	slots   chan struct{}
	cursor  atomic.Uint64
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
	seen := make(map[string]bool)
	for _, route := range routes {
		if !identifier.Semantic(route.Kind) || route.Publish == nil {
			return nil, fault.New(fault.Invalid, "invalid outbox publication route")
		}
		if err := route.Destination.Validate(); err != nil {
			return nil, err
		}
		key := route.Kind + "\x00" + string(route.Destination)
		if seen[key] {
			return nil, fault.New(fault.Duplicate, "outbox publication route already registered")
		}
		seen[key] = true
	}
	return &Publisher{writer: writer, config: config, routes: slices.Clone(routes), slots: make(chan struct{}, config.MaxInFlight)}, nil
}

// PublishOne reserves at most one row; concurrent publishers skip locked rows.
// Every callback is retained until actual exit. SQL cancellation or connection
// loss can release its row lock before that exit; destination deduplication must
// therefore also cover overlapping publications of the same stable identity.
func (p *Publisher) PublishOne(ctx context.Context) (Result, error) {
	if p == nil || p.slots == nil || ctx == nil {
		return Result{}, fault.New(fault.Invalid, "outbox publication requires initialization and context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	select {
	case p.slots <- struct{}{}:
	default:
		return Result{}, fault.New(fault.Conflict, "outbox publication capacity reached")
	}
	defer func() { <-p.slots }()
	operation, cancel := context.WithTimeout(ctx, p.config.OperationTimeout)
	defer cancel()
	now, err := temporal.NewDateTime(p.config.Clock.Now())
	if err != nil {
		return Result{}, err
	}
	fields := outboxstore.MessageFields()
	result := Result{}
	start := p.cursor.Add(1) - 1
	for offset := range len(p.routes) {
		route := p.routes[(start+uint64(offset))%uint64(len(p.routes))]
		err = p.writer.Transaction(operation, func(tx *database.Tx) error {
			found, err := outboxstore.QueryFoundryOutbox().Where(fields.Kind.Eq(route.Kind), fields.Destination.Eq(route.Destination), fields.PublishState.Eq(outbox.Pending), fields.PublishAfter.Lte(now)).OrderBy(fields.CreatedAt.Asc(), fields.ID.Asc()).ForUpdate().SkipLocked().First(operation, tx)
			if err != nil {
				return err
			}
			row, ok := found.Get()
			if !ok {
				return nil
			}
			result = Result{ID: model.IDFromBytes[Publication](row.ID.Bytes()), Found: true, Attempts: row.PublishAttempts}
			var publishErr error
			permanent := false
			if row.PublishAttempts >= p.config.MaxAttempts {
				result.State = outbox.PublicationFailed
			} else {
				result.Attempts++
				publishErr = callback.Isolated("publish outbox message", func() error { return route.Publish(operation, Message{record: row}) })
				result.Failure = publishErr
				permanent = permanentFailure(publishErr)
				switch {
				case publishErr == nil:
					result.State = outbox.Published
				case permanent:
					result.State = outbox.PublicationFailed
				case result.Attempts >= p.config.MaxAttempts:
					result.State = outbox.PublicationFailed
				default:
					result.State = outbox.Pending
				}
			}
			finished, err := temporal.NewDateTime(p.config.Clock.Now())
			if err != nil {
				return err
			}
			draft := outboxstore.MessageDraft{}.SetPublishState(result.State).SetPublishAttempts(result.Attempts)
			switch result.State {
			case outbox.Published:
				draft = draft.SetPublishedAt(finished).SetPublishReason("")
			case outbox.Pending:
				next, err := finished.Add(p.config.RetryDelay)
				if err != nil {
					return err
				}
				draft = draft.SetPublishAfter(next).SetPublishReason("transient")
			default:
				reason := "attempt_limit"
				if permanent {
					reason = "permanent"
				}
				draft = draft.SetPublishReason(reason)
			}
			_, err = outboxstore.QueryFoundryOutbox().Update(operation, tx, row.ID, draft)
			return err
		})
		if err != nil {
			return result, err
		}
		if result.Found {
			result.Committed = true
			break
		}
	}
	if result.Found && p.config.Observe != nil {
		if err := callback.Isolated("observe outbox publication", func() error { return p.config.Observe(operation, result) }); err != nil {
			return result, err
		}
	}
	return result, nil
}
func (p *Publisher) Run(ctx context.Context) error {
	if p == nil || p.slots == nil || ctx == nil {
		return fault.New(fault.Invalid, "outbox publisher requires initialization and context")
	}
	if !p.running.CompareAndSwap(false, true) {
		return fault.New(fault.Conflict, "outbox publisher is already running")
	}
	defer p.running.Store(false)
	for ctx.Err() == nil {
		result, err := p.PublishOne(ctx)
		if err != nil {
			if stopped := ctx.Err(); stopped != nil {
				cancelled := false
				inspection := callback.Isolated("classify outbox cancellation", func() error {
					cancelled = errorgraph.Is(err, stopped)
					return nil
				})
				if inspection != nil {
					return inspection
				}
				if cancelled {
					return nil
				}
			}
			return err
		}
		if !result.Found || result.Failure != nil {
			timer := time.NewTimer(p.config.PollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
	return nil
}
