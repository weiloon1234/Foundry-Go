package recovering

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Sessions = session.Sessions[Member, model.ID[Member]]
type Tokens = token.Tokens[Member, model.ID[Member]]
type Login = auth.PasswordLogin[Member, model.ID[Member], string]
type Revocations = auth.Revocations[Member, model.ID[Member]]

// CredentialRevocations registers all credential guards this domain uses. New
// guards must be included here so every password reset invalidates them.
func CredentialRevocations(provider Provider, sessions *Sessions, tokens *Tokens) (*Revocations, error) {
	web, err := sessions.Revocation()
	if err != nil {
		return nil, err
	}
	api, err := tokens.Revocation()
	if err != nil {
		return nil, err
	}
	return auth.NewRevocations(provider, web, api)
}

// NewLogin uses the same provider/model as recovery and the credential guards.
// Lock is required: issuance rechecks the post-rehash model in its transaction.
func NewLogin(db interface {
	database.Executor
	database.Transactor
}, provider Provider, hasher *password.Hasher) (*Login, error) {
	return auth.NewPasswordLogin(provider, hasher, auth.PasswordModel[Member, string]{
		Lookup: func(ctx context.Context, email string) (value.Optional[Member], error) {
			return QueryRecoveryMembers().Where(MemberFields().Email.Eq(email)).First(ctx, db)
		},
		Lock: func(ctx context.Context, tx *database.Tx, prior Member) (value.Optional[Member], error) {
			return lock(ctx, tx, prior.ID)
		},
		Hash: func(member Member) password.Hash { return member.Password },
		Rehash: func(ctx context.Context, member Member, old, next password.Hash) (value.Optional[Member], error) {
			updated, err := QueryRecoveryMembers().Where(MemberFields().Password.Eq(old)).Update(ctx, db, member.ID, MemberDraft{}.SetPassword(next))
			if errors.Is(err, database.NotFound) {
				return value.Optional[Member]{}, nil
			}
			if err != nil {
				return value.Optional[Member]{}, err
			}
			return value.Set(updated), nil
		},
		RequiresMFA: func(context.Context, Member) (bool, error) { return false, nil },
	}, auth.DefaultConfig())
}
func InvalidateCredentials(ctx context.Context, tx *database.Tx, revocations *Revocations, member Member) error {
	return revocations.Invalidate(ctx, tx, member)
}

func RestrictProof(proof auth.Proof[Member, model.ID[Member]], scopes auth.AccessScopes[Member]) (auth.Proof[Member, model.ID[Member]], error) {
	return proof.WithAccessScopes(scopes)
}

// RecheckPassword returns the current locked member to a sensitive domain change.
// A pending-MFA result verifies only its password; require the existing factor
// before disabling or replacing an enrolled factor.
func RecheckPassword(ctx context.Context, tx *database.Tx, provider Provider, result auth.PasswordResult[Member, model.ID[Member]]) (Member, error) {
	return provider.RecheckPassword(ctx, tx, result)
}
