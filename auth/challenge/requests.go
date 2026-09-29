package challenge

import (
	"context"
	"log/slog"
	"sync"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Issuer is the model/purpose contract implemented by Flow, passwordreset.Reset
// and emailverification.Verification. A requester borrows the configured issuer.
type Issuer[M model.Identifiable, K any, P Purpose] interface {
	Validate() error
	Issue(context.Context, model.Reference[M, K]) (Issued[M, P], error)
}

// RequestCallbacks maps a canonical submitted identifier to a model reference
// and delivers ONLY to the committed issuance's stored subject address. Lookup
// is domain-specific; it grants no authority. Issue locks and checks the current
// model again. Deliver runs after commit, outside database locks, at most once.
// Both run in the requester's owned background dispatch, not in the caller's
// request. Deliver's result is not crash durable: an email/outbox adapter owns
// durable dispatch (for example enqueue a job carrying the issued link).
type RequestCallbacks[M model.Identifiable, K, I any, P Purpose] struct {
	Lookup  func(context.Context, I) (value.Optional[model.Reference[M, K]], error)
	Deliver func(context.Context, Issued[M, P]) error
}

// Requests owns public recovery-request orchestration and bounds all callbacks.
// It returns no token/model/existence flag. Request performs only the
// identifier's rate-limit admission synchronously, identical for existing and
// missing accounts, then hands lookup, issuance and delivery to a bounded
// background dispatch owned by the requester, so response latency does not
// depend on whether the account exists. Operational failures are logged
// safely. Close drains the owned dispatches at shutdown.
type Requests[M model.Identifiable, K, I any, P Purpose] struct {
	issuer    Issuer[M, K, P]
	limiter   ratelimit.Limiter[I]
	callbacks RequestCallbacks[M, K, I, P]
	logger    *slog.Logger
	config    auth.Config
	slots     *admission.Semaphore
	owned     context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	active    int
	idle      chan struct{} // Closed when active returns to zero.
}

func NewRequests[M model.Identifiable, K, I any, P Purpose](issuer Issuer[M, K, P], limiter ratelimit.Limiter[I], callbacks RequestCallbacks[M, K, I, P], logger *slog.Logger, config auth.Config) (*Requests[M, K, I, P], error) {
	if credential.IsNil(issuer) || callbacks.Lookup == nil || callbacks.Deliver == nil || logger == nil {
		return nil, fault.New(fault.Invalid, "recovery requests require issuer, lookup, delivery and logger")
	}
	for _, err := range []error{issuer.Validate(), limiter.Validate(), config.Validate()} {
		if err != nil {
			return nil, err
		}
	}
	owned, cancel := context.WithCancel(context.Background())
	return &Requests[M, K, I, P]{issuer: issuer, limiter: limiter, callbacks: callbacks, logger: logger, config: config, slots: admission.New(config.MaxConcurrent), owned: owned, cancel: cancel}, nil
}
func (r *Requests[M, K, I, P]) Validate() error {
	if r == nil || r.slots == nil {
		return fault.New(fault.Invalid, "recovery requests are not configured")
	}
	return nil
}

// Request uses the same canonical identifier for quota and lookup. Validate and
// normalize it before this call. Quota denial is also an acknowledgement, without
// lookup or token replacement. Rate-authority failures, dispatch capacity
// (fault.Overloaded after a short bounded wait) and a closed requester are
// reported before any lookup, so they never depend on account existence.
// Ingress/IP limiting is separately required at the transport. There are no
// implicit retries, automatic login or plaintext recovery result. A lost/failed
// delivery can leave an undisclosed active link.
func (r *Requests[M, K, I, P]) Request(ctx context.Context, identifier I) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "recovery request requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	admit, cancel := context.WithTimeout(ctx, r.config.Timeout)
	decision, err := r.limiter.Allow(admit, identifier)
	cancel()
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return nil
	}
	if err := r.slots.Acquire(ctx, admission.Wait(r.config.Timeout), r.owned.Done()); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		r.slots.Release()
		return fault.New(fault.Closed, "recovery requests are closed")
	}
	if r.active == 0 {
		r.idle = make(chan struct{})
	}
	r.active++
	r.mu.Unlock()
	// Keep request values (attribution, locale) but not its cancellation: the
	// dispatch belongs to this requester and ends with Close, not the response.
	base := context.WithoutCancel(ctx)
	go func() {
		defer r.finish()
		defer r.slots.Release()
		r.dispatch(base, identifier)
	}()
	return nil
}
func (r *Requests[M, K, I, P]) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active--
	if r.active == 0 {
		close(r.idle)
	}
}

// Wait blocks until every dispatch admitted so far has finished, or ctx ends.
// It does not stop new requests; tests and maintenance tools use it to observe
// completed delivery. A callback ignoring cancellation keeps Wait blocked.
func (r *Requests[M, K, I, P]) Wait(ctx context.Context) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "waiting for recovery requests requires a context")
	}
	r.mu.Lock()
	if r.active == 0 {
		r.mu.Unlock()
		return nil
	}
	idle := r.idle
	r.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dispatch performs lookup, issuance and delivery within one bounded operation.
func (r *Requests[M, K, I, P]) dispatch(base context.Context, identifier I) {
	op, cancel := context.WithTimeout(base, r.config.Timeout)
	defer cancel()
	stop := context.AfterFunc(r.owned, cancel)
	defer stop()
	err := callback.Isolated("recovery request dispatch", func() error {
		found, err := r.callbacks.Lookup(op, identifier)
		if err != nil {
			return err
		}
		reference, present := found.Get()
		if !present {
			return nil
		}
		if err := op.Err(); err != nil {
			return err
		}
		issued, err := r.issuer.Issue(op, reference)
		if err != nil {
			outcome, found, complete := errorgraph.As[*database.Error](err)
			committedOrUnknown := !complete || found && (outcome == nil || outcome.Outcome() == database.Committed || outcome.Outcome() == database.Unknown)
			if !committedOrUnknown && errorgraph.Is(err, auth.Unauthenticated) {
				return nil
			}
			return err
		}
		// Retain the committed model snapshot; never send to submitted input.
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		actual, err := issued.Subject().FoundryIdentity()
		if err != nil {
			return err
		}
		if actual != identity || issued.Token().Validate() != nil || issued.ExpiresAt().IsZero() {
			return fault.New(fault.Invalid, "recovery issuer returned an inconsistent delivery")
		}
		if err := op.Err(); err != nil {
			return err
		}
		return r.callbacks.Deliver(op, issued)
	})
	if err != nil {
		// Neither submitted identifier nor credential appears in routine logs.
		// A broken logger must not affect other dispatches.
		_ = callback.Isolated("recovery request diagnostics", func() error {
			r.logger.ErrorContext(op, "account recovery request failed", slog.String("purpose", string(purpose[P]())), slog.Any("diagnostic", errordiag.Describe(err)))
			return nil
		})
	}
}

// Close stops accepting requests and waits for owned dispatches. When ctx ends
// first, remaining dispatches are canceled and Close returns ctx.Err(); a
// callback that ignores cancellation keeps its slot until it actually exits.
// It is idempotent. Register it with the application's shutdown hooks.
func (r *Requests[M, K, I, P]) Close(ctx context.Context) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "closing recovery requests requires a context")
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	err := r.Wait(ctx)
	r.cancel()
	return err
}
