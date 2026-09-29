package token

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Tokens binds one stored model/provider/guard/source and declared scope ceiling.
// Its guard is constructed once; persisted tokens cannot be rebound by extracting
// an unscoped strategy. Construction performs no I/O.
type Tokens[M model.Identifiable, K any] struct {
	store    *Store
	provider auth.Provider[M, K]
	address  Address
	allowed  auth.AccessScopes[M]
	guard    auth.Guard[M]
	current  auth.CredentialSlot[Info[M, K]]
	observer auth.Observer
}

func New[M model.Identifiable, K any](store *Store, name auth.GuardName, provider auth.Provider[M, K], source auth.CredentialName, allowed auth.AccessScopes[M]) (*Tokens[M, K], error) {
	if err := store.validate(); err != nil {
		return nil, err
	}
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	address := Address{Namespace: store.config.Namespace, Guard: name, Provider: provider.Name(), Model: provider.ModelName()}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	tokens := &Tokens[M, K]{store: store, provider: provider, address: address, allowed: allowed, current: auth.NewCredentialSlot[Info[M, K]]()}
	tokens.guard = auth.DefineGuard(name, provider, auth.DefineStrategy(source, tokens.verify))
	if err := tokens.guard.Validate(); err != nil {
		return nil, err
	}
	return tokens, nil
}
func (t *Tokens[M, K]) Validate() error {
	if t == nil {
		return fault.New(fault.Invalid, "token binding is missing")
	}
	if err := t.store.validate(); err != nil {
		return err
	}
	return t.guard.Validate()
}
func (t *Tokens[M, K]) Guard() auth.Guard[M] {
	if t == nil {
		return auth.Guard[M]{}
	}
	return t.guard
}
func (t *Tokens[M, K]) info(record Record) (Info[M, K], error) {
	if err := record.Validate(t.address); err != nil {
		return Info[M, K]{}, err
	}
	reference, err := t.provider.Parse(record.Subject)
	if err != nil {
		return Info[M, K]{}, err
	}
	scopes, err := restoreScopes[M](record.Scopes)
	if err != nil {
		return Info[M, K]{}, err
	}
	// The binding's current ceiling always applies: a scope removed from the
	// declaration is withdrawn from stored tokens without making them unusable,
	// unlistable or unrefreshable. The stored grant itself is never widened.
	scopes = scopes.Intersect(t.allowed)
	return Info[M, K]{id: ID[M]{value: record.ID}, subject: reference, name: record.Name, scopes: scopes, mode: record.Mode, assurance: record.Assurance, generation: record.Generation, created: record.CreatedAt, issued: record.IssuedAt, lastSeen: record.LastSeenAt, accessExpires: record.AccessExpiresAt, refreshExpires: record.RefreshExpiresAt, expires: record.ExpiresAt, device: record.Device}, nil
}
func (t *Tokens[M, K]) verify(ctx context.Context, raw secret.String) (value.Optional[auth.Proof[M, K]], error) {
	hash, err := HashSecret(raw)
	if err != nil {
		return value.Optional[auth.Proof[M, K]]{}, err
	}
	var result value.Optional[auth.Proof[M, K]]
	err = t.store.execute(ctx, func(op context.Context) error {
		var found value.Optional[Record]
		var err error
		// A refreshed family's previous access token stays valid for the grace
		// period, so requests already in flight with it do not fail.
		if backend, ok := t.store.backend.(GraceBackend); ok && t.store.config.AccessGrace > 0 {
			found, err = backend.LookupWithin(op, t.address, hash, t.store.config.AccessGrace)
		} else {
			found, err = t.store.backend.Lookup(op, t.address, hash, false)
		}
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if !record.AccessHash.Equal(hash) {
			return fault.New(fault.Invalid, "token backend returned a different access credential")
		}
		if record.SupersededAt.IsSet() && t.store.config.AccessGrace == 0 {
			return fault.New(fault.Invalid, "token backend accepted a superseded generation without grace")
		}
		info, err := t.info(record)
		if err != nil {
			return err
		}
		proof, err := auth.NewScopedProof(info.Subject(), info.Assurance(), info.Scopes())
		if err != nil {
			return err
		}
		proof, err = auth.AttachCredential(proof, t.current, info)
		if err != nil {
			return err
		}
		result = value.Set(proof)
		return nil
	})
	if err != nil {
		return value.Optional[auth.Proof[M, K]]{}, err
	}
	return result, nil
}

// Issue persists a trusted verified proof. It does not verify a password or a
// submitted identity. Requested scopes cannot exceed this binding's declaration
// or the input proof's grant. Pending MFA has fixed expiry and cannot refresh.
func (t *Tokens[M, K]) Issue(ctx context.Context, proof auth.Proof[M, K], options IssueOptions[M]) (Issued[M, K], error) {
	if err := t.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	var result Issued[M, K]
	err := t.store.execute(ctx, func(op context.Context) error {
		var err error
		result, err = t.issue(op, proof, options, value.Optional[temporal.DateTime]{})
		return err
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	t.issued(ctx, result)
	return result, nil
}

// issued reports a completed full login after the backend committed it.
func (t *Tokens[M, K]) issued(ctx context.Context, result Issued[M, K]) {
	if t.observer == nil || result.info.assurance != auth.Authenticated {
		return
	}
	t.notify(ctx, auth.EventLogin, result.info.subject, 0)
}
func (t *Tokens[M, K]) notify(ctx context.Context, kind auth.EventKind, subject model.Reference[M, K], count uint64) {
	if t.observer == nil {
		return
	}
	identity, err := subject.Identity()
	if err != nil {
		return
	}
	auth.Notify(ctx, t.observer, auth.Event{Kind: kind, Guard: t.address.Guard, Provider: t.address.Provider, Subject: value.Set(identity), Count: count})
}

// WithObserver returns a binding sharing this guard that reports EventLogin
// for full issuance (including MFA completion), EventLogout for Logout and
// RevokeCurrent, and EventOtherDevicesLoggedOut for RevokeOthers.
func (t *Tokens[M, K]) WithObserver(observer auth.Observer) (*Tokens[M, K], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		return nil, fault.New(fault.Invalid, "token observer is nil")
	}
	if t.observer != nil {
		return nil, fault.New(fault.Duplicate, "token observer already configured")
	}
	next := *t
	next.observer = observer
	return &next, nil
}

func (t *Tokens[M, K]) issue(ctx context.Context, proof auth.Proof[M, K], options IssueOptions[M], deadline value.Optional[temporal.DateTime]) (Issued[M, K], error) {
	if err := t.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	if deadline.IsSet() && !proof.HasIssuanceCheck() {
		return Issued[M, K]{}, fault.New(fault.Invalid, "MFA creation deadline requires a transactional proof check")
	}

	var result Issued[M, K]
	err := func(op context.Context) error {
		if _, err := t.provider.Parse(proof.Identity()); err != nil {
			return err
		}
		if err := proof.Assurance().Validate(); err != nil {
			return err
		}
		if err := validateName(options.Name); err != nil {
			return err
		}
		if !t.allowed.ContainsAll(options.Scopes) {
			return auth.Forbidden
		}
		if previous, scoped := proof.AccessScopes(); scoped && !previous.ContainsAll(options.Scopes) {
			return auth.Forbidden
		}
		mode, policy := Personal, t.store.config.Personal
		if options.Refresh {
			mode, policy = Renewable, t.store.config.Renewable
		}
		if proof.Assurance() == auth.PendingMFA {
			if options.Refresh || options.Scopes.Len() != 0 {
				return fault.New(fault.Invalid, "MFA challenge cannot refresh or grant ordinary scopes")
			}
			mode, policy = Challenge, t.store.config.Challenge
		}
		id, err := model.NewID[Record]()
		if err != nil {
			return err
		}
		prefix := t.store.config.Prefix
		access, accessHash, err := newSecret(prefix)
		if err != nil {
			return err
		}
		var refresh value.Optional[secret.String]
		var refreshHash value.Optional[Digest]
		var limit uint32
		if mode == Renewable {
			raw, hash, err := newSecret(prefix)
			if err != nil {
				return err
			}
			refresh = value.Set(raw)
			refreshHash = value.Set(hash)
			limit = uint32(t.store.config.MaxRotations)
		}
		config := t.store.config
		request := Creation{ID: id, Subject: proof.Identity(), Name: options.Name, Scopes: options.Scopes.Names(), Mode: mode, Assurance: proof.Assurance(), AccessHash: accessHash, RefreshHash: refreshHash, Lifetime: policy, RotationLimit: limit, Maximum: config.MaxPerSubject, PendingMaximum: config.MaxPendingPerSubject, Limit: config.Limit, Device: auth.DeviceFrom(op)}
		if err := request.Validate(t.address); err != nil {
			return err
		}
		var record Record
		if proof.HasIssuanceCheck() {
			backend, ok := t.store.backend.(CheckedBackend)
			if !ok {
				return fault.New(fault.Invalid, "credential backend cannot enforce issuance checks")
			}
			record, err = credential.CheckCreation(op, proof.CheckIssuance, func(check func(context.Context, *database.Tx) error) (Record, error) {
				if until, present := deadline.Get(); present {
					completing, ok := t.store.backend.(CompletionBackend)
					if !ok {
						return Record{}, fault.New(fault.Invalid, "credential backend cannot complete MFA")
					}
					return completing.CreateBefore(op, t.address, request, until, check)
				}
				return backend.CreateChecked(op, t.address, request, check)
			})
		} else {
			record, err = t.store.backend.Create(op, t.address, request)
		}
		if err != nil {
			return err
		}
		expectedRefresh, hasRefresh := refreshHash.Get()
		actualRefresh, actualHasRefresh := record.RefreshHash.Get()
		if record.ID != id || record.Subject != request.Subject || record.Name != request.Name || !slices.Equal(record.Scopes, options.Scopes.Names()) || record.Mode != request.Mode || record.Assurance != request.Assurance || record.Lifetime != request.Lifetime || record.RotationLimit != request.RotationLimit || record.Device != request.Device || record.Generation != 0 || !record.AccessHash.Equal(accessHash) || hasRefresh != actualHasRefresh || (hasRefresh && !actualRefresh.Equal(expectedRefresh)) {
			return fault.New(fault.Invalid, "token backend changed issued credential metadata")
		}
		info, err := t.info(record)
		if err != nil {
			return err
		}
		result = Issued[M, K]{info: info, access: access, refresh: refresh}
		return nil
	}(ctx)
	if err != nil {
		return Issued[M, K]{}, err
	}
	return result, nil
}

// Refresh consumes one renewable secret atomically. Reuse of a consumed secret
// revokes the entire family, including a concurrent successful successor. Clients
// must serialize refresh and reauthenticate after an uncertain/failed outcome;
// the framework never retries or returns partial credentials.
func (t *Tokens[M, K]) Refresh(ctx context.Context, raw secret.String) (Issued[M, K], error) {
	if err := t.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	old, err := HashSecret(raw)
	if err != nil {
		return Issued[M, K]{}, err
	}
	var result Issued[M, K]
	err = t.store.execute(ctx, func(op context.Context) error {
		access, accessHash, err := newSecret(t.store.config.Prefix)
		if err != nil {
			return err
		}
		refresh, refreshHash, err := newSecret(t.store.config.Prefix)
		if err != nil {
			return err
		}
		found, err := t.store.backend.Refresh(op, t.address, old, accessHash, refreshHash)
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		actualRefresh, present := record.RefreshHash.Get()
		if record.Mode != Renewable || record.Assurance != auth.Authenticated || record.Generation == 0 || !record.AccessHash.Equal(accessHash) || !present || !actualRefresh.Equal(refreshHash) {
			return fault.New(fault.Invalid, "token backend returned invalid refreshed metadata")
		}
		info, err := t.info(record)
		if err != nil {
			return err
		}
		result = Issued[M, K]{info: info, access: access, refresh: value.Set(refresh)}
		return nil
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	return result, nil
}
