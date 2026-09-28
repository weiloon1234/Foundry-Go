package multifactor

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

// SecurityChange is a deliberately small event DTO. Never publish Account itself:
// passwords, factor material and credentials do not belong in event payloads.
type SecurityChange struct {
	Account model.ID[Account] `json:"account"`
	Action  mfa.Action        `json:"action"`
}

var SecurityChanged = events.Define[SecurityChange]("accounts.mfa.changed", 1)

type FactorNotice = mfa.Notice[Account, model.ID[Account]]

// RecordSecurityChange uses the actual factor/credential transaction. The
// initiator is captured automatically from ctx separately from affected Account.
// The configured outbox publisher owns delivery after the transaction commits.
func RecordSecurityChange(ctx context.Context, tx *database.Tx, producer *events.Outbox, notice FactorNotice) (outbox.ID[SecurityChange], error) {
	return SecurityChanged.Enqueue(ctx, tx, producer, SecurityChange{Account: notice.Subject().Key(), Action: notice.Action()})
}
func WithSecurityEvents(factors *Factors, producer *events.Outbox) (*Factors, error) {
	return factors.WithObserver(mfa.Observer[Account, model.ID[Account]]{Changed: func(ctx context.Context, tx *database.Tx, notice FactorNotice) error {
		_, err := RecordSecurityChange(ctx, tx, producer, notice)
		return err
	}})
}

// RetireAccount is application-domain orchestration after an administrative
// policy has authorized this operation. The caller owns the outer transaction.
func RetireAccount(ctx context.Context, tx *database.Tx, factors *Factors, account Account) error {
	if _, err := factors.RetireIn(ctx, tx, account.FoundryReference()); err != nil {
		return err
	}
	_, err := QueryMfaAccounts().Delete(ctx, tx, account.ID)
	return err
}
