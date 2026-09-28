package session

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	credentialcore "github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Sessions binds one stored model/provider/guard/source to a shared store. Its
// Guard is constructed once; persisted credentials cannot be rebound to a different
// guard name by extracting an unscoped strategy. Construction performs no I/O.
type Sessions[M model.Identifiable, K any] struct {
	store    *Store
	provider auth.Provider[M, K]
	address  Address
	guard    auth.Guard[M]
}

func New[M model.Identifiable, K any](store *Store, name auth.GuardName, provider auth.Provider[M, K], source auth.CredentialName) (*Sessions[M, K], error) {
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
	sessions := &Sessions[M, K]{store: store, provider: provider, address: address}
	strategy := auth.DefineStrategy(source, sessions.verify)
	sessions.guard = auth.DefineGuard(name, provider, strategy)
	if err := sessions.guard.Validate(); err != nil {
		return nil, err
	}
	return sessions, nil
}
func (s *Sessions[M, K]) Validate() error {
	if s == nil {
		return fault.New(fault.Invalid, "session binding is missing")
	}
	if err := s.store.validate(); err != nil {
		return err
	}
	return s.guard.Validate()
}
func (s *Sessions[M, K]) Guard() auth.Guard[M] {
	if s == nil {
		return auth.Guard[M]{}
	}
	return s.guard
}
func (s *Sessions[M, K]) info(record Record) (Info[M, K], error) {
	if err := record.Validate(s.address); err != nil {
		return Info[M, K]{}, err
	}
	reference, err := s.provider.Parse(record.Subject)
	if err != nil {
		return Info[M, K]{}, err
	}
	return Info[M, K]{id: ID[M]{value: record.ID}, subject: reference, assurance: record.Assurance, remembered: record.Remember, created: record.CreatedAt, lastSeen: record.LastSeenAt, idleExpires: record.IdleExpiresAt, expires: record.ExpiresAt}, nil
}
func (s *Sessions[M, K]) verify(ctx context.Context, credential secret.String) (value.Optional[auth.Proof[M, K]], error) {
	hash, err := HashSecret(credential)
	if err != nil {
		return value.Optional[auth.Proof[M, K]]{}, err
	}
	var proof value.Optional[auth.Proof[M, K]]
	err = s.store.execute(ctx, func(op context.Context) error {
		found, err := s.store.backend.Lookup(op, s.address, hash, true)
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if !record.Hash.Equal(hash) {
			return fault.New(fault.Invalid, "session backend returned a different credential")
		}
		info, err := s.info(record)
		if err != nil {
			return err
		}
		verified, err := auth.NewProof(info.Subject(), info.Assurance())
		if err != nil {
			return err
		}
		proof = value.Set(verified)
		return nil
	})
	if err != nil {
		return value.Optional[auth.Proof[M, K]]{}, err
	}
	return proof, nil
}

// Issue persists a verified proof. This is a trusted login boundary, not password
// verification. A proof must come from a trusted authentication flow; a parsed
// client identity or request DTO must never be promoted to a proof here.
func (s *Sessions[M, K]) Issue(ctx context.Context, proof auth.Proof[M, K], options IssueOptions) (Issued[M, K], error) {
	if err := s.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	var result Issued[M, K]
	err := s.store.execute(ctx, func(op context.Context) error {
		var err error
		result, err = s.issue(op, proof, options, value.Optional[temporal.DateTime]{})
		return err
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	return result, nil
}

func (s *Sessions[M, K]) issue(ctx context.Context, proof auth.Proof[M, K], options IssueOptions, deadline value.Optional[temporal.DateTime]) (Issued[M, K], error) {
	if err := s.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	if deadline.IsSet() && !proof.HasIssuanceCheck() {
		return Issued[M, K]{}, fault.New(fault.Invalid, "MFA creation deadline requires a transactional proof check")
	}

	var result Issued[M, K]
	err := func(op context.Context) error {
		if _, scoped := proof.AccessScopes(); scoped {
			return fault.New(fault.Invalid, "scoped credential proofs cannot issue unscoped sessions")
		}
		if _, err := s.provider.Parse(proof.Identity()); err != nil {
			return err
		}
		if err := proof.Assurance().Validate(); err != nil {
			return err
		}
		policy := s.store.config.Regular
		if options.Remember {
			policy = s.store.config.Remembered
		}
		if proof.Assurance() == auth.PendingMFA {
			if options.Remember {
				return fault.New(fault.Invalid, "pending MFA cannot request remembered sessions")
			}
			policy = s.store.config.Pending
		}
		id, err := model.NewID[Record]()
		if err != nil {
			return err
		}
		credential, hash, err := newSecret()
		if err != nil {
			return err
		}
		request := Creation{ID: id, Subject: proof.Identity(), Hash: hash, Assurance: proof.Assurance(), Remember: options.Remember, Lifetime: policy, Maximum: s.store.config.MaxPerSubject}
		var record Record
		if proof.HasIssuanceCheck() {
			backend, ok := s.store.backend.(CheckedBackend)
			if !ok {
				return fault.New(fault.Invalid, "credential backend cannot enforce issuance checks")
			}
			record, err = credentialcore.CheckCreation(op, proof.CheckIssuance, func(check func(context.Context, *database.Tx) error) (Record, error) {
				if until, present := deadline.Get(); present {
					completing, ok := s.store.backend.(CompletionBackend)
					if !ok {
						return Record{}, fault.New(fault.Invalid, "credential backend cannot complete MFA")
					}
					return completing.CreateBefore(op, s.address, request, until, check)
				}
				return backend.CreateChecked(op, s.address, request, check)
			})
		} else {
			record, err = s.store.backend.Create(op, s.address, request)
		}
		if err != nil {
			return err
		}
		if record.ID != id || record.Subject != request.Subject || !record.Hash.Equal(hash) || record.Assurance != request.Assurance || record.Remember != request.Remember || record.Lifetime != request.Lifetime {
			return fault.New(fault.Invalid, "session backend changed issued credential metadata")
		}
		info, err := s.info(record)
		if err != nil {
			return err
		}
		result = Issued[M, K]{info: info, secret: credential}
		return nil
	}(ctx)
	if err != nil {
		return Issued[M, K]{}, err
	}
	return result, nil
}

// Rotate atomically changes a live session's secret. The previous secret becomes
// invalid, while session ID, assurance, creation time and absolute expiry persist.
// Parallel rotations have at most one winner. Failed/uncertain writes are not retried.
func (s *Sessions[M, K]) Rotate(ctx context.Context, credential secret.String) (Issued[M, K], error) {
	if err := s.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	old, err := HashSecret(credential)
	if err != nil {
		return Issued[M, K]{}, err
	}
	var result Issued[M, K]
	err = s.store.execute(ctx, func(op context.Context) error {
		next, hash, err := newSecret()
		if err != nil {
			return err
		}
		found, err := s.store.backend.Rotate(op, s.address, old, hash)
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if !record.Hash.Equal(hash) {
			return fault.New(fault.Invalid, "session backend returned a different rotated credential")
		}
		info, err := s.info(record)
		if err != nil {
			return err
		}
		result = Issued[M, K]{info: info, secret: next}
		return nil
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	return result, nil
}
func (s *Sessions[M, K]) Revoke(ctx context.Context, credential secret.String) (bool, error) {
	if err := s.Validate(); err != nil {
		return false, err
	}
	hash, err := HashSecret(credential)
	if err != nil {
		return false, err
	}
	var removed bool
	err = s.store.execute(ctx, func(op context.Context) error {
		var err error
		removed, err = s.store.backend.Revoke(op, s.address, hash)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// RevokeID removes a listed session belonging to this exact subject and guard.
// The identifier is not authority; authorize the caller before choosing a subject.
func (s *Sessions[M, K]) RevokeID(ctx context.Context, reference model.Reference[M, K], id ID[M]) (bool, error) {
	if err := s.Validate(); err != nil {
		return false, err
	}
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "session revocation requires an identifier")
	}
	var removed bool
	err := s.store.execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := s.provider.Parse(identity); err != nil {
			return err
		}
		removed, err = s.store.backend.RevokeID(op, s.address, identity, id.value)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (s *Sessions[M, K]) RevokeAll(ctx context.Context, reference model.Reference[M, K]) (uint64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	var count uint64
	err := s.store.execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := s.provider.Parse(identity); err != nil {
			return err
		}
		count, err = s.store.backend.RevokeAll(op, s.address, identity)
		if err == nil && count > MaxSessions {
			return fault.New(fault.Invalid, "session backend exceeded subject capacity")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// List returns at most MaxSessions live sessions for this model identity. It
// exposes typed metadata, never stored hashes or raw secrets. It does not itself
// authorize the calling user to inspect another user; use an ordinary policy.
func (s *Sessions[M, K]) List(ctx context.Context, reference model.Reference[M, K]) ([]Info[M, K], error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var result []Info[M, K]
	err := s.store.execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := s.provider.Parse(identity); err != nil {
			return err
		}
		records, err := s.store.backend.List(op, s.address, identity, MaxSessions)
		if err != nil {
			return err
		}
		if len(records) > MaxSessions {
			return fault.New(fault.Invalid, "session backend exceeded result limit")
		}
		result = make([]Info[M, K], len(records))
		seen := make(map[model.ID[Record]]bool, len(records))
		for i, record := range records {
			if record.Subject != identity || seen[record.ID] {
				return fault.New(fault.Invalid, "session backend returned foreign or duplicate metadata")
			}
			seen[record.ID] = true
			result[i], err = s.info(record)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Sessions[M, K]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPageSize {
		return 0, fault.New(fault.Invalid, "invalid session prune page size")
	}
	var count uint64
	err := s.store.execute(ctx, func(op context.Context) error {
		var err error
		count, err = s.store.backend.Prune(op, s.address, limit)
		if err == nil && count > uint64(limit) {
			return fault.New(fault.Invalid, "session backend exceeded prune limit")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
