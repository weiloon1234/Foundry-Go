package auditqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Approval struct {
	Reason string `json:"reason"`
}

var Approved = audit.Define[Approval]("account.approved", 1)

// Approve records the explicit domain decision and its concrete stored subject.
// The caller owns the business transaction; no extra model lookup is necessary.
func Approve(ctx context.Context, tx *database.Tx, recorder *audit.Recorder, account Account, reason string) (audit.ActionID[Approval], error) {
	return audit.RecordFor(ctx, tx, recorder, Approved, account.FoundryReference(), Approval{Reason: reason})
}

// EmailAfter demonstrates typed history inspection. Audit data is stored data;
// an application's response DTO chooses its own presentation deliberately.
func EmailAfter(changes AccountChanges) (value.Optional[string], error) {
	captured, err := changes.Audit(AccountAuditPolicy{})
	if err != nil {
		return value.Optional[string]{}, err
	}
	fields, err := AccountAuditFields(captured)
	if err != nil {
		return value.Optional[string]{}, err
	}
	email, present := fields.Email.Get()
	if !present {
		return value.Optional[string]{}, nil
	}
	return email.After().Get()
}
