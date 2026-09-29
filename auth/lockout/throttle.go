package lockout

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Throttle owns one factor's failed-attempt policy, retaining the submitted key
// type. Its callbacks must not issue credentials: Run checks current lock state
// after verification before returning true. Already admitted attempts can overlap;
// use request-rate limits to bound that work independently.
type Throttle[K any] struct {
	store      *Store
	definition *definition[K]
	observer   func(context.Context, Notice[K]) error
}

func (t Throttle[K]) Name() Name     { return (Declaration[K]{t.definition}).Name() }
func (t Throttle[K]) Policy() Policy { return (Declaration[K]{t.definition}).Policy() }

// Limits returns the client-aware limits when this throttle came from DefineLogin.
func (t Throttle[K]) Limits() (Limits, bool) { return (Declaration[K]{t.definition}).Limits() }
func (t Throttle[K]) Validate() error {
	if t.store == nil || t.store.gate == nil {
		return fault.New(fault.Invalid, "lockout throttle is not bound")
	}
	return (Declaration[K]{t.definition}).Validate()
}
func (t Throttle[K]) address(key K) (Key, error) {
	logical, err := t.definition.codec.Encode(key)
	if err != nil {
		return Key{}, err
	}
	if len(logical) > t.store.config.MaxKeyBytes {
		return Key{}, fault.New(fault.Invalid, "encoded login key exceeds its limit")
	}
	return NewKey(t.store.config.Namespace, t.Name(), logical)
}

// Run verifies one credential attempt. False,nil records a failure; true,nil
// conditionally clears old failures. An error or abnormal callback exit records
// neither and returns no authority. Backend uncertainty never falls back or
// retries, and a late lock/expiry prevents a successful callback from succeeding.
// A pending-MFA password proof verifies only that password stage; use a separate
// declaration for MFA attempts so success here cannot clear MFA failures.
func (t Throttle[K]) Run(ctx context.Context, key K, verify func(context.Context) (bool, error)) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	if verify == nil {
		return false, fault.New(fault.Invalid, "lockout requires a verifier")
	}
	if t.definition.limits != nil {
		return t.runClient(ctx, key, verify)
	}
	verified := false
	err := t.store.gate.Execute(ctx, func(op context.Context) error {
		address, err := t.address(key)
		if err != nil {
			return err
		}
		generation, err := NewGeneration()
		if err != nil {
			return err
		}
		if err := op.Err(); err != nil {
			return err
		}
		admission, err := t.store.backend.LockoutBegin(op, address, t.Policy(), generation)
		if err != nil {
			return &unavailable{err}
		}
		if err := admission.Validate(t.Policy()); err != nil {
			return &unavailable{err}
		}
		if err := op.Err(); err != nil {
			return err
		}
		if err := decisionError(admission.Decision); err != nil {
			return err
		}
		verified, err = verify(op)
		if err != nil {
			return err
		}
		if err := op.Err(); err != nil {
			return err
		}
		outcome := Failed
		if verified {
			outcome = Succeeded
		}
		decision, err := t.store.backend.LockoutFinish(op, address, t.Policy(), admission.Snapshot, outcome)
		if err != nil {
			return &unavailable{err}
		}
		if err := decision.Validate(t.Policy()); err != nil {
			return &unavailable{err}
		}
		if outcome == Succeeded && decision.Triggered {
			return &unavailable{fault.New(fault.Internal, "successful attempt started a lock")}
		}
		if err := op.Err(); err != nil {
			return err
		}
		if decision.Triggered && t.observer != nil {
			if err := callback.Isolated("lockout observer", func() error { return t.observer(op, Notice[K]{key: key, retryAfter: decision.RetryAfter}) }); err != nil {
				return &Rejection{retryAfter: decision.RetryAfter, cause: err, triggered: true}
			}
		}
		return decisionError(decision)
	})
	if err != nil {
		return false, err
	}
	return verified, nil
}

// Reset is an explicit administrative/account-recovery operation. Authorize its
// caller. It invalidates outstanding attempts; ordinary login success uses the
// revision check in Run instead. It does not alter request-rate-limit quotas.
// A DefineLogin throttle clears the account ceiling and the pair for the
// current request's client; other clients' pair windows expire on their own.
func (t Throttle[K]) Reset(ctx context.Context, key K) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	if t.definition.limits != nil {
		return t.resetClient(ctx, key)
	}
	changed := false
	err := t.store.gate.Execute(ctx, func(op context.Context) error {
		address, err := t.address(key)
		if err != nil {
			return err
		}
		if err := op.Err(); err != nil {
			return err
		}
		changed, err = t.store.backend.LockoutReset(op, address, t.Policy())
		if err != nil {
			return &unavailable{err}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}
