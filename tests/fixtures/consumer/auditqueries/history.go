package auditqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
)

type Approval struct {
	Reason string `json:"reason"`
}

var Approved = audit.Define[Approval]("account.approved", 1)

// Approve records the explicit domain decision and its concrete stored subject.
// The caller owns the business transaction; no extra model lookup is necessary.
func Approve(ctx context.Context, tx *database.Tx, recorder *audit.Recorder, account Account, reason string) (audit.ActionID[Approval], error) {
	return audit.RecordFor(ctx, tx, recorder, Approved, AccountIdentity.ReferenceOf(account), Approval{Reason: reason})
}
