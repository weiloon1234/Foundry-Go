package challenge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Model owns only domain mapping. Lock must use a generated ForUpdate query on
// the supplied transaction and return an owned full model. Binding uses stored
// fields, never read getters or a stale model passed from an HTTP request.
type Model[M model.Identifiable, K any] struct {
	Lock    func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Binding func(M) (Binding, error)
}

type Flow[M model.Identifiable, K any, P Purpose] struct {
	store    *Store
	provider auth.Provider[M, K]
	model    Model[M, K]
	address  Address
	lifetime time.Duration
}

func New[M model.Identifiable, K any, P Purpose](store *Store, provider auth.Provider[M, K], binding Model[M, K], lifetime time.Duration) (*Flow[M, K, P], error) {
	for _, err := range []error{store.Validate(), provider.Validate(), ValidateLifetime(lifetime)} {
		if err != nil {
			return nil, err
		}
	}
	if binding.Lock == nil || binding.Binding == nil {
		return nil, fault.New(fault.Invalid, "challenge model requires lock and binding callbacks")
	}
	address := Address{Namespace: store.config.Namespace, Provider: provider.Name(), Model: provider.ModelName(), Purpose: purpose[P]()}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	return &Flow[M, K, P]{store: store, provider: provider, model: binding, address: address, lifetime: lifetime}, nil
}
func (f *Flow[M, K, P]) Validate() error {
	if f == nil {
		return fault.New(fault.Invalid, "challenge flow is not configured")
	}
	return f.store.Validate()
}

// Issued keeps delivery information private to ordinary formatting and JSON.
// Use the locked Subject snapshot's email when constructing a delivery message.
// No built-in email delivery is performed here, nor is publication crash durable.
type Issued[M any, P Purpose] struct {
	subject M
	token   Token[M, P]
	expires temporal.DateTime
}

func (i Issued[M, P]) Subject() M                   { return i.subject }
func (i Issued[M, P]) Token() Token[M, P]           { return i.token }
func (i Issued[M, P]) ExpiresAt() temporal.DateTime { return i.expires }
func (Issued[M, P]) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("issued challenge")) }

func (f *Flow[M, K, P]) locked(ctx context.Context, tx *database.Tx, identity model.Identity) (M, Binding, error) {
	reference, err := f.provider.Parse(identity)
	if err != nil {
		return *new(M), Binding{}, err
	}
	found, err := f.model.Lock(ctx, tx, reference.Key())
	if err != nil {
		return *new(M), Binding{}, err
	}
	subject, present := found.Get()
	if !present {
		return *new(M), Binding{}, auth.Unauthenticated
	}
	actual, err := f.provider.CheckModel(ctx, subject)
	if err != nil {
		return *new(M), Binding{}, err
	}
	got, err := actual.Identity()
	if err != nil {
		return *new(M), Binding{}, err
	}
	if got != identity {
		return *new(M), Binding{}, fault.New(fault.Invalid, "challenge lookup returned a different identity")
	}
	binding, err := f.model.Binding(subject)
	if err != nil {
		return *new(M), Binding{}, err
	}
	if binding.IsZero() {
		return *new(M), Binding{}, fault.New(fault.Invalid, "challenge model returned an empty binding")
	}
	return subject, binding, nil
}

// Issue replaces the previous link for this model/provider/purpose. The reference
// selects a model; it does not authenticate the caller. Public request handlers
// must throttle, use a uniform response and deliver only to the stored address.
func (f *Flow[M, K, P]) Issue(ctx context.Context, reference model.Reference[M, K]) (Issued[M, P], error) {
	if err := f.Validate(); err != nil {
		return Issued[M, P]{}, err
	}
	var result Issued[M, P]
	err := f.store.gate.Execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := f.provider.Parse(identity); err != nil {
			return err
		}
		raw, hash, err := credential.New()
		if err != nil {
			return err
		}
		var subject M
		var binding Binding
		called := false
		var callbackError error
		record, err := f.store.backend.Issue(op, f.address, identity, Digest{hash: hash}, f.lifetime, func(inner context.Context, tx *database.Tx) (Binding, error) {
			if called || tx == nil || callbackError != nil {
				if callbackError == nil {
					callbackError = fault.New(fault.Invalid, "invalid challenge preparation callback")
				}
				return Binding{}, callbackError
			}
			called = true
			callbackError = callback.Isolated("challenge model preparation", func() error {
				var err error
				subject, binding, err = f.locked(op, tx, identity)
				if err != nil {
					return err
				}
				return op.Err()
			})
			return binding, callbackError
		})
		if err != nil || callbackError != nil {
			return errors.Join(err, callbackError)
		}
		if err := record.Validate(f.address); err != nil {
			return err
		}
		if !called || record.Subject != identity || !record.Hash.hash.Equal(hash) || !record.Binding.Equal(binding) || record.ExpiresAt.UTC().Sub(record.CreatedAt.UTC()) != f.lifetime {
			return fault.New(fault.Invalid, "challenge backend returned a different issuance")
		}
		result = Issued[M, P]{subject: subject, token: Token[M, P]{value: raw}, expires: record.ExpiresAt}
		return nil
	})
	if err != nil {
		return Issued[M, P]{}, err
	}
	return result, nil
}

// Consume executes an identity-preserving model mutation inside the consumption
// transaction. Missing, expired, replaced and stale-state links return the same
// Unauthenticated error. No proof/session/token is issued by consuming a link.
func (f *Flow[M, K, P]) Consume(ctx context.Context, token Token[M, P], apply func(context.Context, *database.Tx, M) (M, error)) (M, error) {
	if err := f.Validate(); err != nil {
		return *new(M), err
	}
	if apply == nil {
		return *new(M), fault.New(fault.Invalid, "challenge consumption requires an action")
	}
	var result M
	err := f.store.gate.Execute(ctx, func(op context.Context) error {
		hash, err := HashSecret(token.value)
		if err != nil {
			return err
		}
		called := false
		var callbackError error
		var applied Record
		found, err := f.store.backend.Consume(op, f.address, hash, func(inner context.Context, tx *database.Tx, record Record) error {
			if called || tx == nil || callbackError != nil {
				if callbackError == nil {
					callbackError = fault.New(fault.Invalid, "invalid challenge consumption callback")
				}
				return callbackError
			}
			called = true
			callbackError = callback.Isolated("challenge model action", func() error {
				if err := record.Validate(f.address); err != nil {
					return err
				}
				if !record.Hash.Equal(hash) {
					return fault.New(fault.Invalid, "challenge backend selected a different credential")
				}
				subject, binding, err := f.locked(op, tx, record.Subject)
				if err != nil {
					return err
				}
				if !binding.Equal(record.Binding) {
					return auth.Unauthenticated
				}
				result, err = apply(op, tx, subject)
				if err != nil {
					return err
				}
				identity, err := result.FoundryIdentity()
				if err != nil {
					return err
				}
				if identity != record.Subject {
					return fault.New(fault.Invalid, "challenge action changed model identity")
				}
				if err := op.Err(); err != nil {
					return err
				}
				applied = record
				return nil
			})
			return callbackError
		})
		if err != nil || callbackError != nil {
			return errors.Join(err, callbackError)
		}
		record, present := found.Get()
		if !present {
			if called {
				return fault.New(fault.Invalid, "challenge backend discarded a completed action")
			}
			return auth.Unauthenticated
		}
		if !called || record != applied {
			return fault.New(fault.Invalid, "challenge backend returned a different consumption")
		}
		return nil
	})
	if err != nil {
		return *new(M), err
	}
	return result, nil
}
func (f *Flow[M, K, P]) Revoke(ctx context.Context, reference model.Reference[M, K]) (bool, error) {
	if err := f.Validate(); err != nil {
		return false, err
	}
	var removed bool
	err := f.store.gate.Execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := f.provider.Parse(identity); err != nil {
			return err
		}
		removed, err = f.store.backend.Revoke(op, f.address, identity)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
func (f *Flow[M, K, P]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := f.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid challenge prune limit")
	}
	var removed uint64
	err := f.store.gate.Execute(ctx, func(op context.Context) error {
		var err error
		removed, err = f.store.backend.Prune(op, f.address, limit)
		if err == nil && removed > uint64(limit) {
			return fault.New(fault.Invalid, "challenge prune exceeded its limit")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
