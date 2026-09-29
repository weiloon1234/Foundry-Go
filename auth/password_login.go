package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
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
	factors    EnrolledFactors[M]
	observer   Observer
	confirm    *lockout.Throttle[ConfirmationKey]
}

// ConfirmationKey is the lockout key of one subject's password confirmations:
// a fixed-size digest of the provider and stored identity, never the identity.
type ConfirmationKey string

// ConfirmationKeys is the key codec for confirmation lockout declarations, for
// example lockout.Define("accounts.confirm", auth.ConfirmationKeys(), lockout.DefaultPolicy()).
func ConfirmationKeys() keyspace.Codec[ConfirmationKey] {
	return keyspace.StringKeys[ConfirmationKey]()
}

// WithConfirmationLockout counts failed Confirm attempts per subject, so a
// stolen session cookie cannot brute-force the password through a
// confirmation screen. Use a declaration separate from the login throttle.
// A locked subject fails Confirm with lockout.Locked (HTTP 429) before hashing.
func (l *PasswordLogin[M, K, I]) WithConfirmationLockout(throttle lockout.Throttle[ConfirmationKey]) (*PasswordLogin[M, K, I], error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if err := throttle.Validate(); err != nil {
		return nil, err
	}
	if l.confirm != nil {
		return nil, fault.New(fault.Duplicate, "password confirmation already has lockout protection")
	}
	result := *l
	result.confirm = &throttle
	return &result, nil
}

func (l *PasswordLogin[M, K, I]) confirmationKey(subject M) (ConfirmationKey, error) {
	identity, err := subject.FoundryIdentity()
	if err != nil {
		return "", err
	}
	encoded, err := identity.KeyJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(string(l.provider.Name()) + "\x00" + identity.ModelName() + "\x00" + encoded))
	return ConfirmationKey(hex.EncodeToString(digest[:])), nil
}

// EnrolledFactors reports whether a model currently has a confirmed second
// factor. mfa.Factors implements it from the model's stored enabled state.
type EnrolledFactors[M any] interface {
	HasEnrolledFactor(context.Context, M) (bool, error)
}

// WithSecondFactor links enrolled factors at construction: login and issuance
// rechecks then require MFA whenever RequiresMFA or an enrolled factor says so,
// so a RequiresMFA callback that forgets the factor state cannot bypass MFA.
func (l *PasswordLogin[M, K, I]) WithSecondFactor(factors EnrolledFactors[M]) (*PasswordLogin[M, K, I], error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if credential.IsNil(factors) {
		return nil, fault.New(fault.Invalid, "password login second factor is missing")
	}
	if l.factors != nil {
		return nil, fault.New(fault.Duplicate, "password login already has second-factor enrollment")
	}
	result := *l
	result.factors = factors
	return &result, nil
}

// WithObserver returns a login view that reports EventFailed (with Subject only
// when an account matched) and EventLockout (when a failure starts a lock).
// Successful logins are reported by the session/token store that issues them.
func (l *PasswordLogin[M, K, I]) WithObserver(observer Observer) (*PasswordLogin[M, K, I], error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		return nil, fault.New(fault.Invalid, "password login observer is nil")
	}
	if l.observer != nil {
		return nil, fault.New(fault.Duplicate, "password login observer already configured")
	}
	result := *l
	result.observer = observer
	return &result, nil
}

// requiresMFA combines the domain policy with linked factor enrollment.
func (l *PasswordLogin[M, K, I]) requiresMFA(ctx context.Context, subject M) (bool, error) {
	required, err := l.model.RequiresMFA(ctx, subject)
	if err != nil || required || l.factors == nil {
		return required, err
	}
	return l.factors.HasEnrolledFactor(ctx, subject)
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
// zero result. Rehash uses compare-and-swap. Losing it to a concurrent login's
// rehash re-verifies the password against the winning hash and continues; if
// the password itself changed, the attempt fails without counting as a lockout
// failure and never issues credentials for the replaced password.
func (l *PasswordLogin[M, K, I]) Authenticate(ctx context.Context, key I, plain password.Plaintext) (PasswordResult[M, K], error) {
	if err := l.Validate(); err != nil {
		return PasswordResult[M, K]{}, err
	}
	var result PasswordResult[M, K]
	var matched model.Identity
	err := l.gate.Execute(ctx, func(op context.Context) error {
		var err error
		result, err = l.authenticateProtected(op, key, plain, &matched)
		return err
	})
	if err != nil {
		l.report(ctx, err, matched)
		return PasswordResult[M, K]{}, err
	}
	return result, nil
}

// report emits failure/lockout observations after the attempt completed.
func (l *PasswordLogin[M, K, I]) report(ctx context.Context, err error, matched model.Identity) {
	if l.observer == nil {
		return
	}
	event := Event{Kind: EventFailed, Provider: l.provider.Name()}
	if !matched.IsZero() {
		event.Subject = value.Set(matched)
	}
	if rejection, found, complete := errorgraph.As[*lockout.Rejection](err); complete && found {
		if !rejection.Triggered() {
			return
		}
		event.Kind = EventLockout
	} else if !errorgraph.Is(err, Unauthenticated) {
		return
	}
	Notify(ctx, l.observer, event)
}

// Confirm re-verifies plain against subject's stored hash for a password
// confirmation step. Pass the current model from the request's guard. It runs
// no lookup or lockout (the caller is authenticated; apply request-rate limits)
// and returns Unauthenticated for a mismatch or unusable hash.
func (l *PasswordLogin[M, K, I]) Confirm(ctx context.Context, subject M, plain password.Plaintext) error {
	if err := l.Validate(); err != nil {
		return err
	}
	check := func(op context.Context) (bool, error) {
		if err := plain.Validate(); err != nil {
			return false, nil
		}
		matched, err := l.hasher.Check(op, plain, l.model.Hash(subject))
		if errors.Is(err, fault.Invalid) {
			return false, nil
		}
		return matched, err
	}
	return l.gate.Execute(ctx, func(op context.Context) error {
		var matched bool
		var err error
		if l.confirm == nil {
			matched, err = check(op)
		} else {
			key, keyErr := l.confirmationKey(subject)
			if keyErr != nil {
				return keyErr
			}
			matched, err = l.confirm.Run(op, key, check)
		}
		if err != nil {
			return err
		}
		if !matched {
			return Unauthenticated
		}
		return op.Err()
	})
}

func (l *PasswordLogin[M, K, I]) rejectWithWork(ctx context.Context, plain password.Plaintext) error {
	if err := l.hasher.DummyCheck(ctx, plain); err != nil {
		return err
	}
	return Unauthenticated
}

func (l *PasswordLogin[M, K, I]) authenticate(ctx context.Context, key I, plain password.Plaintext, matched *model.Identity) (PasswordResult[M, K], error) {
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
	identity, err := subject.FoundryIdentity()
	if err != nil {
		return fail(err)
	}
	*matched = identity
	stored := l.model.Hash(subject)
	verified, err := l.hasher.Check(ctx, plain, stored)
	if errors.Is(err, fault.Invalid) {
		return fail(l.rejectWithWork(ctx, plain))
	}
	if err != nil {
		return fail(err)
	}
	if !verified {
		return fail(Unauthenticated)
	}
	reference, err := l.provider.Parse(identity)
	if err != nil {
		return fail(err)
	}
	if err := l.provider.checkEligibility(ctx, subject); err != nil {
		return fail(err)
	}
	subject, err = l.rehash(ctx, key, subject, identity, stored, plain)
	if err != nil {
		return fail(err)
	}
	required, err := l.requiresMFA(ctx, subject)
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
