package session

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxImpersonation bounds any impersonation session's absolute lifetime.
const MaxImpersonation = 24 * time.Hour

// ActorSessionEnded is the cause Resume reports (as auth.Unauthenticated) when
// the impersonation ended but the original actor's session had been revoked or
// had expired meanwhile: the actor must log in again.
var ActorSessionEnded = fault.New(fault.Missing, "the impersonating actor's session has ended")

// Impersonation lets an authenticated actor of one session binding act as a
// model of another (or the same) binding, for support and administration. The
// impersonation session records the actor, its guard and its own session, so
// the original actor stays accessible and the session is visibly marked:
// Info.Impersonated, Sessions.RequireNotImpersonating and ConfirmCurrent refuse
// sensitive operations and mint no other credential (CurrentProof,
// RevokeOthers). Sessions are bounded by a maximum duration, never remembered,
// counted apart from the subject's own sessions, and end when the actor's own
// session is revoked or expires, so both bindings must share one store.
//
// Authorize the actor (for example with a policy on the target model) before
// Start: the binding does not decide who may impersonate whom. Observers
// receive EventImpersonationStarted and EventImpersonationStopped, which an
// audit or outbox observer can persist.
type Impersonation[A model.Identifiable, AK any, M model.Identifiable, K any] struct {
	actors   *Sessions[A, AK]
	targets  *Sessions[M, K]
	maximum  time.Duration
	observer auth.Observer
}

func NewImpersonation[A model.Identifiable, AK any, M model.Identifiable, K any](actors *Sessions[A, AK], targets *Sessions[M, K], maximum time.Duration) (*Impersonation[A, AK, M, K], error) {
	if err := actors.Validate(); err != nil {
		return nil, err
	}
	if err := targets.Validate(); err != nil {
		return nil, err
	}
	if maximum <= 0 || maximum > MaxImpersonation || maximum%time.Microsecond != 0 {
		return nil, fault.New(fault.Invalid, "impersonation maximum must be a positive microsecond duration up to 24 hours")
	}
	// The store checks every impersonation against its actor's own session, so
	// revoking or expiring the actor ends it; both must live in one store.
	if actors.store != targets.store {
		return nil, fault.New(fault.Invalid, "impersonation requires actors and targets to share one session store")
	}
	return &Impersonation[A, AK, M, K]{actors: actors, targets: targets, maximum: maximum}, nil
}

func (i *Impersonation[A, AK, M, K]) Validate() error {
	if i == nil {
		return fault.New(fault.Invalid, "impersonation is not configured")
	}
	if err := i.actors.Validate(); err != nil {
		return err
	}
	return i.targets.Validate()
}

// WithObserver returns a copy reporting impersonation start and stop.
func (i *Impersonation[A, AK, M, K]) WithObserver(observer auth.Observer) (*Impersonation[A, AK, M, K], error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		return nil, fault.New(fault.Invalid, "impersonation observer is nil")
	}
	if i.observer != nil {
		return nil, fault.New(fault.Duplicate, "impersonation observer already configured")
	}
	next := *i
	next.observer = observer
	return &next, nil
}

// Actors and Targets return the bound session bindings.
func (i *Impersonation[A, AK, M, K]) Actors() *Sessions[A, AK] { return i.actors }
func (i *Impersonation[A, AK, M, K]) Targets() *Sessions[M, K] { return i.targets }

// sameBinding reports whether actors and targets share one guard binding, so
// they share one browser cookie.
func (i *Impersonation[A, AK, M, K]) sameBinding() bool {
	return i.actors.address == i.targets.address
}

// Start issues an impersonation session for target, marked with the current
// actor session of the actors guard, lasting at most duration (and the
// configured maximum). The actor's own session is not revoked; Resume or Stop
// returns to it. Nested impersonation and self-impersonation are refused; the
// target must exist and be eligible through its provider.
func (i *Impersonation[A, AK, M, K]) Start(ctx context.Context, target model.Reference[M, K], duration time.Duration) (Issued[M, K], error) {
	if err := i.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	if duration <= 0 || duration > i.maximum || duration%time.Microsecond != 0 {
		return Issued[M, K]{}, fault.New(fault.Invalid, "impersonation duration exceeds its bound")
	}
	actor, err := i.actors.Current(ctx)
	if err != nil {
		return Issued[M, K]{}, err
	}
	if actor.Impersonated() || actor.Assurance() != auth.Authenticated {
		return Issued[M, K]{}, auth.ImpersonationForbidden
	}
	actorIdentity, err := actor.Subject().Identity()
	if err != nil {
		return Issued[M, K]{}, err
	}
	targetIdentity, err := target.Identity()
	if err != nil {
		return Issued[M, K]{}, err
	}
	if i.sameBinding() && actorIdentity == targetIdentity {
		return Issued[M, K]{}, fault.New(fault.Invalid, "an actor cannot impersonate itself")
	}
	if _, err := i.targets.provider.Resolve(ctx, target); err != nil {
		return Issued[M, K]{}, err
	}
	proof, err := auth.NewProof(target, auth.Authenticated)
	if err != nil {
		return Issued[M, K]{}, err
	}
	regular := i.targets.store.config.Regular
	lifetime := Lifetime{Idle: min(regular.Idle, duration), Absolute: duration, Sliding: regular.Sliding}
	if err := lifetime.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	grant := impersonationGrant{impersonator: Impersonator{Subject: actorIdentity, Guard: i.actors.address.Guard, Session: actor.ID().value}, lifetime: lifetime}
	var result Issued[M, K]
	err = i.targets.store.execute(ctx, func(op context.Context) error {
		var err error
		result, err = i.targets.issueWith(op, proof, IssueOptions{}, value.Optional[temporal.DateTime]{}, value.Set(grant))
		return err
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	i.notify(ctx, auth.EventImpersonationStarted, targetIdentity, actorIdentity)
	return result, nil
}

// Actor returns the original actor behind the current impersonation session of
// the targets guard, loaded through the actors provider, or an omitted value
// when the current session is not impersonated.
func (i *Impersonation[A, AK, M, K]) Actor(ctx context.Context) (value.Optional[A], error) {
	if err := i.Validate(); err != nil {
		return value.Optional[A]{}, err
	}
	impersonator, _, err := i.current(ctx)
	if err != nil {
		return value.Optional[A]{}, err
	}
	recorded, present := impersonator.Get()
	if !present {
		return value.Optional[A]{}, nil
	}
	reference, err := i.actors.provider.Parse(recorded.Subject)
	if err != nil {
		return value.Optional[A]{}, err
	}
	actor, err := i.actors.provider.Resolve(ctx, reference)
	if err != nil {
		return value.Optional[A]{}, err
	}
	return value.Set(actor), nil
}

// current reads the current target session's impersonation marker and checks
// that it belongs to this binding's actors guard.
func (i *Impersonation[A, AK, M, K]) current(ctx context.Context) (value.Optional[Impersonator], Info[M, K], error) {
	info, err := i.targets.Current(ctx)
	if err != nil {
		return value.Optional[Impersonator]{}, Info[M, K]{}, err
	}
	recorded, present := info.Impersonator().Get()
	if present && recorded.Guard != i.actors.address.Guard {
		return value.Optional[Impersonator]{}, Info[M, K]{}, auth.Forbidden
	}
	return info.Impersonator(), info, nil
}

// end revokes the current impersonation session and reports the stop.
func (i *Impersonation[A, AK, M, K]) end(ctx context.Context) (Impersonator, error) {
	impersonator, info, err := i.current(ctx)
	if err != nil {
		return Impersonator{}, err
	}
	recorded, present := impersonator.Get()
	if !present {
		return Impersonator{}, auth.Forbidden
	}
	if _, err := i.targets.RevokeID(ctx, info.Subject(), info.ID()); err != nil {
		return Impersonator{}, err
	}
	if identity, err := info.Subject().Identity(); err == nil {
		i.notify(ctx, auth.EventImpersonationStopped, identity, recorded.Subject)
	}
	return recorded, nil
}

// Stop ends the current impersonation session. Use it when the actor keeps its
// own credential (another cookie or token); its session is untouched.
func (i *Impersonation[A, AK, M, K]) Stop(ctx context.Context) error {
	if err := i.Validate(); err != nil {
		return err
	}
	_, err := i.end(ctx)
	return err
}

// Resume ends the current impersonation session and returns the original
// actor's session with a fresh secret, for flows where actor and target share
// one cookie. The actor session recorded at Start is rotated in place: it keeps
// its ID, creation time, absolute expiry and remember policy, so impersonation
// can never extend an actor's session. When the actor's session was revoked,
// rotated or expired meanwhile, or the actor is no longer eligible, the
// impersonation still ends and Resume fails with auth.Unauthenticated caused by
// ActorSessionEnded: the actor must log in again.
func (i *Impersonation[A, AK, M, K]) Resume(ctx context.Context) (Issued[A, AK], error) {
	if err := i.Validate(); err != nil {
		return Issued[A, AK]{}, err
	}
	backend, ok := i.actors.store.backend.(ResumptionBackend)
	if !ok {
		return Issued[A, AK]{}, fault.New(fault.Invalid, "credential backend cannot resume impersonation atomically")
	}
	recorded, err := i.end(ctx)
	if err != nil {
		return Issued[A, AK]{}, err
	}
	reference, err := i.actors.provider.Parse(recorded.Subject)
	if err != nil {
		return Issued[A, AK]{}, err
	}
	var original Record
	var found bool
	err = i.actors.store.execute(ctx, func(op context.Context) error {
		records, err := i.actors.store.backend.List(op, i.actors.address, recorded.Subject, MaxPageSize)
		if err != nil {
			return err
		}
		for _, candidate := range records {
			if candidate.ID == recorded.Session && candidate.Subject == recorded.Subject {
				original, found = candidate, true
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return Issued[A, AK]{}, err
	}
	ended := auth.Unauthenticated.WithCause(ActorSessionEnded)
	if !found || original.Impersonator.IsSet() || original.Assurance != auth.Authenticated {
		return Issued[A, AK]{}, ended
	}
	credential, hash, err := newSecret()
	if err != nil {
		return Issued[A, AK]{}, err
	}
	var result Issued[A, AK]
	err = i.actors.store.execute(ctx, func(op context.Context) error {
		rotated, err := backend.ResumeActor(op, i.actors.address, original, hash, func(check context.Context, _ *database.Tx) error {
			// The actor must still be eligible; the provider decides.
			_, err := i.actors.provider.Resolve(check, reference)
			if err != nil && errorgraph.Is(err, auth.Unauthenticated) {
				return errors.Join(err, ActorSessionEnded)
			}
			return err
		})
		if err != nil {
			return err
		}
		record, present := rotated.Get()
		if !present {
			return ended
		}
		if record.ID != original.ID || record.Subject != original.Subject || !record.Hash.Equal(hash) || record.CreatedAt != original.CreatedAt || record.ExpiresAt != original.ExpiresAt || record.Remember != original.Remember || record.Impersonator.IsSet() {
			return fault.New(fault.Invalid, "session backend changed the resumed actor session")
		}
		info, err := i.actors.info(record)
		if err != nil {
			return err
		}
		result = Issued[A, AK]{info: info, secret: credential}
		return nil
	})
	if err != nil {
		return Issued[A, AK]{}, err
	}
	return result, nil
}

func (i *Impersonation[A, AK, M, K]) notify(ctx context.Context, kind auth.EventKind, subject, actor model.Identity) {
	if i.observer == nil {
		return
	}
	auth.Notify(ctx, i.observer, auth.Event{Kind: kind, Guard: i.targets.address.Guard, Provider: i.targets.address.Provider, Subject: value.Set(subject), Impersonator: value.Set(actor)})
}
