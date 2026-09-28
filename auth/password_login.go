package auth

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
)

// PasswordLogin verifies one typed login key using a shared model provider and
// hasher. Construction performs no I/O. Share it across requests. Config bounds
// its callbacks; the hasher independently bounds expensive Argon2 operations.
// WithLockout adds shared failed-attempt protection; request-rate limiting runs
// before expensive verification. Credential issuance is a subsequent operation.
// Its proof carries a
// transaction check that locks and revalidates the current hash, eligibility and
// MFA policy before credential insertion. Guards recheck models at use time.
type PasswordLogin[M model.Identifiable, K, I any] struct {
	provider   Provider[M, K]
	hasher     *password.Hasher
	model      PasswordModel[M, I]
	gate       *credential.Gate
	protection *lockout.Throttle[I]
}

func NewPasswordLogin[M model.Identifiable, K, I any](provider Provider[M, K], hasher *password.Hasher, binding PasswordModel[M, I], config Config) (*PasswordLogin[M, K, I], error) {
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	if err := hasher.Validate(); err != nil {
		return nil, err
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &PasswordLogin[M, K, I]{provider: provider, hasher: hasher, model: binding, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (l *PasswordLogin[M, K, I]) Validate() error {
	if l == nil || l.gate == nil {
		return fault.New(fault.Invalid, "password login is not configured")
	}
	return nil
}

// Authenticate returns one concrete model and its proof, with no second model
// lookup. Missing, disabled, unusable-password and wrong-password cases expose
// Unauthenticated. Operational failures remain errors. Every failure returns a
// zero result. Rehash uses compare-and-swap; losing it rejects this attempt,
// rather than issuing credentials for a potentially changed password.
func (l *PasswordLogin[M, K, I]) Authenticate(ctx context.Context, key I, plain password.Plaintext) (PasswordResult[M, K], error) {
	if err := l.Validate(); err != nil {
		return PasswordResult[M, K]{}, err
	}
	var result PasswordResult[M, K]
	err := l.gate.Execute(ctx, func(op context.Context) error {
		var err error
		result, err = l.authenticateProtected(op, key, plain)
		return err
	})
	if err != nil {
		return PasswordResult[M, K]{}, err
	}
	return result, nil
}

func (l *PasswordLogin[M, K, I]) rejectWithWork(ctx context.Context, plain password.Plaintext) error {
	if err := l.hasher.DummyCheck(ctx, plain); err != nil {
		return err
	}
	return Unauthenticated
}

func (l *PasswordLogin[M, K, I]) authenticate(ctx context.Context, key I, plain password.Plaintext) (PasswordResult[M, K], error) {
	if err := plain.Validate(); err != nil {
		return PasswordResult[M, K]{}, Unauthenticated
	}
	fail := func(err error) (PasswordResult[M, K], error) { return PasswordResult[M, K]{}, err }
	found, err := l.model.Lookup(ctx, key)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	subject, exists := found.Get()
	if !exists {
		return fail(l.rejectWithWork(ctx, plain))
	}
	stored := l.model.Hash(subject)
	matched, err := l.hasher.Check(ctx, plain, stored)
	if errors.Is(err, fault.Invalid) {
		return fail(l.rejectWithWork(ctx, plain))
	}
	if err != nil {
		return fail(err)
	}
	if !matched {
		return fail(Unauthenticated)
	}
	identity, err := subject.FoundryIdentity()
	if err != nil {
		return fail(err)
	}
	reference, err := l.provider.Parse(identity)
	if err != nil {
		return fail(err)
	}
	if err := l.provider.checkEligibility(ctx, subject); err != nil {
		return fail(err)
	}
	subject, err = l.rehash(ctx, subject, identity, stored, plain)
	if err != nil {
		return fail(err)
	}
	required, err := l.model.RequiresMFA(ctx, subject)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	assurance := Authenticated
	if required {
		assurance = PendingMFA
	}
	proof, err := NewProof(reference, assurance)
	if err != nil {
		return fail(err)
	}
	return l.checkedResult(subject, proof)
}
