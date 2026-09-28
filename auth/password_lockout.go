package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// WithLockout returns a login view using the same typed login key for lookup and
// failed-attempt protection. It preserves the original callback capacity and
// shared hasher. Use a password-specific declaration; MFA/recovery use separate
// policies so successful password verification cannot clear their failures.
func (l *PasswordLogin[M, K, I]) WithLockout(throttle lockout.Throttle[I]) (*PasswordLogin[M, K, I], error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if err := throttle.Validate(); err != nil {
		return nil, err
	}
	if l.protection != nil {
		return nil, fault.New(fault.Duplicate, "password login already has lockout protection")
	}
	result := *l
	result.protection = &throttle
	return &result, nil
}
func (l *PasswordLogin[M, K, I]) authenticateProtected(ctx context.Context, key I, plain password.Plaintext) (PasswordResult[M, K], error) {
	if l.protection == nil {
		return l.authenticate(ctx, key, plain)
	}
	var result PasswordResult[M, K]
	ok, err := l.protection.Run(ctx, key, func(op context.Context) (bool, error) {
		var err error
		result, err = l.authenticate(op, key, plain)
		if errorgraph.Is(err, Unauthenticated) {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		return PasswordResult[M, K]{}, err
	}
	if !ok {
		return PasswordResult[M, K]{}, Unauthenticated
	}
	return result, nil
}
