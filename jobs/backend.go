package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State describes an authority's retained execution record.
type State string

const (
	Waiting   State = "waiting"
	Blocked   State = "blocked"
	Reserved  State = "reserved"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

func (s State) Terminal() bool { return s == Succeeded || s == Failed || s == Cancelled }

// Reason is a bounded operational classification, never arbitrary handler error
// text or a serialized panic. Inspect handler failures through explicit callbacks.
type Reason string

const (
	NoReason         Reason = ""
	HandlerFailed    Reason = "handler_failed"
	HandlerPanicked  Reason = "handler_panicked"
	PayloadInvalid   Reason = "payload_invalid"
	Unregistered     Reason = "unregistered"
	AttemptLimit     Reason = "attempt_limit"
	LeaseExpired     Reason = "lease_expired"
	TimedOut         Reason = "timed_out"
	CancelRequested  Reason = "cancel_requested"
	WorkerStopped    Reason = "worker_stopped"
	RateLimited      Reason = "rate_limited"
	DependencyFailed Reason = "dependency_failed"
	ManuallyRetried  Reason = "manually_retried"
	// DeliveryLimit fails a job whose reservations repeatedly expired before an
	// attempt started, such as a payload that crashes its process while decoding.
	DeliveryLimit Reason = "delivery_limit"
	// ExceptionLimit fails a job that reached Policy.MaxExceptions.
	ExceptionLimit Reason = "exception_limit"
	// RetryExpired fails a job whose Policy.RetryUntil deadline passed.
	RetryExpired Reason = "retry_expired"
	// NotTriggered cancels a workflow catch job when no member failed.
	NotTriggered Reason = "not_triggered"
)

// exception reports reasons counted against Policy.MaxExceptions.
func (r Reason) exception() bool {
	return r == HandlerFailed || r == HandlerPanicked || r == TimedOut || r == ExceptionLimit
}

// CountsException reports whether a finished attempt with this reason is a
// handler exception for Policy.MaxExceptions. Backends increment their per-record
// exception counter when they apply such a result.
func (r Reason) CountsException() bool { return r.exception() }

func (r Reason) Validate() error {
	switch r {
	case NoReason, HandlerFailed, HandlerPanicked, PayloadInvalid, Unregistered, AttemptLimit, LeaseExpired, TimedOut, CancelRequested, WorkerStopped, RateLimited, DependencyFailed, ManuallyRetried, DeliveryLimit, ExceptionLimit, RetryExpired, NotTriggered:
		return nil
	}
	return fault.New(fault.Invalid, "invalid job failure classification")
}

// Target is the complete adapter address for a retained dispatch. Cancellation
// compares all fields atomically, including after a deduplication record expires.
type Target struct {
	ID      ExecutionID
	Name    Name
	Version Version
}

func (t Target) Validate() error {
	if t.ID.IsZero() || !identifier.Semantic(string(t.Name)) || t.Version == 0 {
		return fault.New(fault.Invalid, "invalid job target")
	}
	return nil
}

// Ownership identifies one reservation, not just its stable dispatch identity.
// Backends must atomically compare its owner AND live expiry on every mutation.
// These adapter values must never be carried in a job payload or logged.
type Ownership struct {
	id    ExecutionID
	owner lease.Owner
}

func NewOwnership(id ExecutionID, owner lease.Owner) (Ownership, error) {
	result := Ownership{id: id, owner: owner}
	return result, result.Validate()
}
func (p Ownership) ID() ExecutionID    { return p.id }
func (p Ownership) Owner() lease.Owner { return p.owner }
func (p Ownership) Validate() error {
	if p.id.IsZero() {
		return fault.New(fault.Invalid, "job ownership requires a dispatch identity")
	}
	return p.owner.Validate()
}
func (Ownership) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job ownership")) }

// Reservation is returned only after an atomic ready-to-leased transition.
// Attempts excludes the reserved attempt until JobStart confirms it.
// Reservation.Exceptions counts attempts that ended as handler exceptions
// (Reason.CountsException) for Policy.MaxExceptions.
type Reservation struct {
	Retries    uint32
	Envelope   Envelope
	Ownership  Ownership
	Attempts   uint32
	Exceptions uint32
	ExpiresAt  time.Time
}

// LeaseStatus distinguishes loss from a cancellation request. A canceled job
// remains leased while its handler exits; cancellation is not immediate release.
type LeaseStatus struct {
	Owned                 bool
	CancellationRequested bool
}

// Result is applied once by the current owner. Waiting schedules retry/release;
// Succeeded and Failed finish execution. Cancellation requests take precedence.
// Retrying does not replace the stable ID or reset the attempt counter.
//
// Refund returns a started attempt to the budget because the worker interrupted
// it (shutdown or drain deadline) rather than the job failing. It is valid only
// with State Waiting. Built-in backends decrement a running record's attempts;
// a custom backend that ignores it consumes the attempt, as before.
type Result struct {
	State  State
	Delay  time.Duration
	Reason Reason
	Refund bool
}

func (r Result) Validate() error {
	if r.State != Waiting && r.State != Succeeded && r.State != Failed {
		return fault.New(fault.Invalid, "invalid job completion state")
	}
	if r.Refund && r.State != Waiting {
		return fault.New(fault.Invalid, "only a released job can refund its attempt")
	}
	if r.Delay < 0 || r.Delay > MaxDelay || (r.State != Waiting && r.Delay != 0) {
		return fault.New(fault.Invalid, "invalid job completion delay")
	}
	if r.State == Succeeded && r.Reason != NoReason {
		return fault.New(fault.Invalid, "successful job cannot have a failure reason")
	}
	return r.Reason.Validate()
}

// Transition is bounded operational history. It never embeds payloads/errors.
type Transition struct {
	State   State
	At      time.Time
	Attempt uint32
	Reason  Reason
	Retry   uint32
}

// Record is an owned inspection snapshot. Envelope access remains explicit.
type Record struct {
	Retries               uint32
	LastRetry             RetryToken
	Workflow              WorkflowID
	Position              uint32
	Envelope              Envelope
	State                 State
	Attempts              uint32
	Exceptions            uint32
	AvailableAt           time.Time
	LeaseExpiresAt        time.Time
	CancellationRequested bool
	CreatedAt             time.Time
	FinishedAt            time.Time
	History               []Transition
}

// Backend is one atomic queue authority. It owns its clock, bounded retention
// and history. Mutations must not be implicitly retried. An enqueue error may
// mean acceptance was unconfirmed: retry the same captured Pending and identity.
// Duplicate identities with identical envelopes are no-ops during retention;
// differing envelopes fail with fault.Conflict. Terminal identity retention is
// finite and must exceed the application's publication/redelivery recovery window.
//
// Reserve reclaims expired leases without resetting attempts. Start is idempotent
// for its live owner. Finish, Renew and Start reject expired or superseded owners.
// Adapters must isolate queues/namespaces, honor context, bound data/metadata and
// return owned snapshots. Backend lifecycle is owned outside this interface.
type Backend interface {
	JobList(context.Context, Key, ListOptions) (Page, error)
	JobWorkflow(context.Context, Key, WorkflowEnvelope) (bool, error)
	JobCancelWorkflow(context.Context, Key, WorkflowID) (bool, error)
	JobEnqueue(context.Context, Key, Envelope) (bool, error)
	JobReserve(context.Context, Key, lease.Owner, time.Duration) (value.Optional[Reservation], error)
	JobStart(context.Context, Key, Ownership) (uint32, error)
	JobRenew(context.Context, Key, Ownership, time.Duration) (LeaseStatus, error)
	JobFinish(context.Context, Key, Ownership, Result) (bool, error)
	JobCancel(context.Context, Key, Target) (bool, error)
	JobInspect(context.Context, Key, ExecutionID) (value.Optional[Record], error)
}

// WakeBackend is optional. An in-process authority closes the channel returned
// for key after its next accepted enqueue, retry or workflow in that queue, so
// idle local workers subscribed to it poll immediately instead of waiting for
// their adaptive idle interval. Other queues' work never wakes them.
type WakeBackend interface {
	JobWakeup(Key) <-chan struct{}
}

// ErrQueuePolicy reports that a queue was created with a different QueueConfig
// than this process uses, typically during a configuration rollout. Nothing was
// changed. It is an operator problem, not a permanent property of the job:
// outbox publication keeps such rows pending and retries them.
var ErrQueuePolicy = fault.New(fault.Internal, "job queue policy differs from this process; use the same QueueConfig on every process sharing the queue")

// ErrLegacyLayout reports a queue still stored in an older layout that this
// release does not migrate implicitly. Stop or drain every process of the
// previous release, then migrate explicitly (`jobs migrate-layout`,
// Dispatcher.MigrateLayout). Nothing was changed.
var ErrLegacyLayout = fault.New(fault.Internal, "job queue uses an older storage layout; stop or drain every process of the previous release, then run `jobs migrate-layout` for this queue")

// LayoutMigrator is optional: an authority whose stored queue layout changed
// migrates a queue only when explicitly asked. migrated=false means the queue
// already used the current layout (or did not exist); repeating is safe.
type LayoutMigrator interface {
	JobMigrateLayout(context.Context, Key) (bool, error)
}

// ErrOwnershipLost means the reservation can no longer authorize a mutation.
var ErrOwnershipLost = fault.New(fault.Conflict, "job reservation ownership lost")
var ErrCancelled = fault.New(fault.Closed, "job cancellation requested")

// ValidateOperation is shared by adapters at every operation boundary.
func ValidateOperation(ctx context.Context, key Key) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "job operation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return key.Validate()
}
func ValidateEnqueue(ctx context.Context, key Key, envelope Envelope) error {
	if err := ValidateOperation(ctx, key); err != nil {
		return err
	}
	if err := envelope.Validate(); err != nil {
		return err
	}
	if key.Queue() != envelope.Queue() {
		return fault.New(fault.Invalid, "job envelope does not belong to this queue")
	}
	return nil
}
