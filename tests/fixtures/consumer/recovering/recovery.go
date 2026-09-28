package recovering

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Provider = auth.Provider[Member, model.ID[Member]]
type Reset = passwordreset.Reset[Member, model.ID[Member]]
type Verification = emailverification.Verification[Member, model.ID[Member]]

func Members(db database.Executor) Provider {
	return auth.DefineProvider("recovery.members", (Member{}).FoundryReference(), func(ctx context.Context, id model.ID[Member]) (value.Optional[Member], error) {
		return QueryRecoveryMembers().Find(ctx, db, id)
	}, func(_ context.Context, member Member) (bool, error) { return member.Enabled, nil })
}
func lock(ctx context.Context, tx *database.Tx, id model.ID[Member]) (value.Optional[Member], error) {
	return QueryRecoveryMembers().ForUpdate().Find(ctx, tx, id)
}
func email(member Member) string                             { return member.Email }
func emailRevision(member Member) challenge.Revision[Member] { return member.EmailRevision }

// invalidate is deliberately mandatory. It must use tx for credential-state
// changes; ordinary session/token RevokeAll methods open separate transactions
// and therefore do not satisfy this callback's atomicity contract.
func NewReset(store *challenge.Store, provider Provider, hasher *password.Hasher, invalidate func(context.Context, *database.Tx, Member) error) (*Reset, error) {
	return passwordreset.New(store, provider, hasher, passwordreset.Model[Member, model.ID[Member]]{
		Lock: lock, Email: email, EmailRevision: emailRevision, Hash: func(member Member) password.Hash { return member.Password },
		SetPassword: func(ctx context.Context, tx *database.Tx, member Member, next password.Hash) (Member, error) {
			return QueryRecoveryMembers().Update(ctx, tx, member.ID, MemberDraft{}.SetPassword(next))
		},
		ValidatePassword: func(_ context.Context, plain password.Plaintext) error {
			if len(plain.Secret().Reveal()) < 12 {
				return fault.New(fault.Invalid, "fixture password requires twelve bytes")
			}
			return nil
		},
		Invalidate: invalidate,
	}, passwordreset.DefaultLifetime)
}
func NewVerification(store *challenge.Store, provider Provider) (*Verification, error) {
	return emailverification.New(store, provider, emailverification.Model[Member, model.ID[Member]]{
		Lock: lock, Email: email, EmailRevision: emailRevision, Verified: func(member Member) bool { return member.EmailVerified },
		MarkVerified: func(ctx context.Context, tx *database.Tx, member Member) (Member, error) {
			return QueryRecoveryMembers().Update(ctx, tx, member.ID, MemberDraft{}.SetEmailVerified(true))
		},
	}, emailverification.DefaultLifetime)
}
func ResetPassword(ctx context.Context, reset *Reset, token passwordreset.Token[Member], plain password.Plaintext) (Member, error) {
	return reset.Complete(ctx, token, plain)
}
func VerifyEmail(ctx context.Context, verification *Verification, token emailverification.Token[Member]) (Member, error) {
	return verification.Complete(ctx, token)
}
