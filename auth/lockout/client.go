package lockout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// unknownClient groups attempts that carry no trusted client address, such as
// CLI or worker contexts. They share one pair window per account. Known IPv6
// clients are grouped per /64 network, matching HTTP IP rate limits.
const unknownClient = "unknown"

type clientTarget struct {
	key    Key
	policy Policy
	// clears reports whether a successful attempt clears this window. The
	// Address ceiling is never cleared by success: an attacker's own valid
	// account must not reset the source's spraying budget.
	clears bool
}

// clientTargets derives the pair, account and (when an address is known)
// address keys. Logical keys use distinct prefixes and fixed-size digests under
// one family name, so they cannot collide with each other or exceed bounds.
func (t Throttle[K]) clientTargets(ctx context.Context, key K) ([]clientTarget, error) {
	logical, err := t.definition.codec.Encode(key)
	if err != nil {
		return nil, err
	}
	if len(logical) > t.store.config.MaxKeyBytes {
		return nil, fault.New(fault.Invalid, "encoded login key exceeds its limit")
	}
	sum := sha256.Sum256([]byte(logical))
	account := hex.EncodeToString(sum[:])
	client, known := unknownClient, false
	if ip := attribution.FromContext(ctx).Request().IP.Unmap(); ip.IsValid() {
		// One IPv6 subscriber usually controls a /64; per-address keys would let
		// it rotate through unlimited windows. IPv4 stays per address.
		if ip.Is6() {
			if prefix, err := ip.Prefix(64); err == nil {
				ip = prefix.Addr()
			}
		}
		client, known = ip.String(), true
	}
	limits := t.definition.limits
	pair, err := NewKey(t.store.config.Namespace, t.Name(), "client:"+client+":"+account)
	if err != nil {
		return nil, err
	}
	owner, err := NewKey(t.store.config.Namespace, t.Name(), "account:"+account)
	if err != nil {
		return nil, err
	}
	targets := []clientTarget{{pair, limits.PerClient, true}, {owner, limits.Account, true}}
	if address, enabled := limits.Address.Get(); enabled && known {
		source, err := NewKey(t.store.config.Namespace, t.Name(), "address:"+client)
		if err != nil {
			return nil, err
		}
		targets = append(targets, clientTarget{source, address, false})
	}
	return targets, nil
}

// runClient applies Run's contract to every client-aware window: any lock denies
// before verification; the verifier runs once; a failure is recorded in every
// admitted window; a success clears the pair and account windows (subject to
// their revision checks) and still fails if one of them locked or expired
// meanwhile. The observer sees at most one notice per attempt.
func (t Throttle[K]) runClient(ctx context.Context, key K, verify func(context.Context) (bool, error)) (bool, error) {
	verified := false
	err := t.store.gate.Execute(ctx, func(op context.Context) error {
		targets, err := t.clientTargets(op, key)
		if err != nil {
			return err
		}
		snapshots := make([]Snapshot, len(targets))
		var denied *Rejection
		for i, target := range targets {
			generation, err := NewGeneration()
			if err != nil {
				return err
			}
			if err := op.Err(); err != nil {
				return err
			}
			admission, err := t.store.backend.LockoutBegin(op, target.key, target.policy, generation)
			if err != nil {
				return &unavailable{err}
			}
			if err := admission.Validate(target.policy); err != nil {
				return &unavailable{err}
			}
			if admission.Decision.Status == StatusLocked {
				if denied == nil || admission.Decision.RetryAfter > denied.retryAfter {
					denied = &Rejection{retryAfter: admission.Decision.RetryAfter}
				}
				continue
			}
			if err := decisionError(admission.Decision); err != nil {
				return err
			}
			snapshots[i] = admission.Snapshot
		}
		if err := op.Err(); err != nil {
			return err
		}
		if denied != nil {
			return denied
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
		var locked, triggered Decision
		expired := false
		for i, target := range targets {
			if verified && !target.clears {
				continue
			}
			decision, err := t.store.backend.LockoutFinish(op, target.key, target.policy, snapshots[i], outcome)
			if err != nil {
				return &unavailable{err}
			}
			if err := decision.Validate(target.policy); err != nil {
				return &unavailable{err}
			}
			if outcome == Succeeded && decision.Triggered {
				return &unavailable{fault.New(fault.Internal, "successful attempt started a lock")}
			}
			switch decision.Status {
			case StatusLocked:
				if decision.RetryAfter > locked.RetryAfter {
					locked = decision
				}
				if decision.Triggered && decision.RetryAfter > triggered.RetryAfter {
					triggered = decision
				}
			case StatusExpired:
				expired = true
			}
		}
		if err := op.Err(); err != nil {
			return err
		}
		if triggered.Triggered {
			rejection := &Rejection{retryAfter: locked.RetryAfter, triggered: true}
			if t.observer != nil {
				rejection.cause = callback.Isolated("lockout observer", func() error {
					return t.observer(op, Notice[K]{key: key, retryAfter: triggered.RetryAfter})
				})
			}
			return rejection
		}
		if locked.Status == StatusLocked {
			return &Rejection{retryAfter: locked.RetryAfter}
		}
		if expired {
			return Expired
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return verified, nil
}

// resetClient clears the account ceiling and the current client's pair window.
func (t Throttle[K]) resetClient(ctx context.Context, key K) (bool, error) {
	changed := false
	err := t.store.gate.Execute(ctx, func(op context.Context) error {
		targets, err := t.clientTargets(op, key)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if !target.clears {
				continue
			}
			if err := op.Err(); err != nil {
				return err
			}
			reset, err := t.store.backend.LockoutReset(op, target.key, target.policy)
			if err != nil {
				return &unavailable{err}
			}
			changed = changed || reset
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}
