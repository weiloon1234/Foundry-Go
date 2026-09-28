package challenge

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
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
// Its result is not crash durable: an email/outbox adapter owns durable dispatch.
type RequestCallbacks[M model.Identifiable, K, I any, P Purpose] struct {
	Lookup  func(context.Context, I) (value.Optional[model.Reference[M, K]], error)
	Deliver func(context.Context, Issued[M, P]) error
}

// Requests owns public recovery-request orchestration and bounds all callbacks.
// It returns no token/model/existence flag. After recipient admission, absent,
// ineligible and failed-delivery requests have the same successful return shape.
// Operational failures are logged safely; there is no constant-time promise.
type Requests[M model.Identifiable, K, I any, P Purpose] struct {
	issuer    Issuer[M, K, P]
	limiter   ratelimit.Limiter[I]
	callbacks RequestCallbacks[M, K, I, P]
	logger    *slog.Logger
	gate      *credential.Gate
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
	return &Requests[M, K, I, P]{issuer: issuer, limiter: limiter, callbacks: callbacks, logger: logger, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (r *Requests[M, K, I, P]) Validate() error {
	if r == nil || r.gate == nil {
		return fault.New(fault.Invalid, "recovery requests are not configured")
	}
	return nil
}

// Request uses the same canonical identifier for quota and lookup. Validate and
// normalize it before this call. Quota denial is also an acknowledgement, without
// lookup or token replacement. Rate-authority/capacity errors happen before lookup
// and remain errors; ingress/IP limiting is separately required at the transport.
// There are no implicit retries, detached work, automatic login or plaintext
// recovery result. A lost/failed delivery can leave an undisclosed active link.
func (r *Requests[M, K, I, P]) Request(ctx context.Context, identifier I) error {
	if err := r.Validate(); err != nil {
		return err
	}
	admitted := false
	err := r.gate.Execute(ctx, func(op context.Context) error {
		decision, err := r.limiter.Allow(op, identifier)
		if err != nil {
			return err
		}
		admitted = true
		if !decision.Allowed {
			return nil
		}
		err = callback.Isolated("recovery request dispatch", func() error {
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
			// A broken logger must not turn account-dependent failure into an oracle.
			_ = callback.Isolated("recovery request diagnostics", func() error {
				r.logger.ErrorContext(op, "account recovery request failed", slog.String("purpose", string(purpose[P]())), slog.Any("error", fault.Wrap(fault.Internal, "recovery dispatch failed", err)))
				return nil
			})
		}
		return op.Err()
	})
	// A server-side timeout after admission must not reveal whether lookup found
	// an account. Caller cancellation/deadline still propagates; no work escapes.
	if admitted && ctx.Err() == nil {
		return nil
	}
	return err
}
