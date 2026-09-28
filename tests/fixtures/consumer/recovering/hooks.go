package recovering

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// memberHooks owns the email revision for all ordinary generated model writes.
// This model uses canonical stored email without a write mutator. If later
// observers/mutators change it, Saved checks the final stored change and rolls
// back any mutation that omitted the accompanying revision/verification reset.
func memberHooks() MemberHooks {
	return MemberHooks{
		Saving: func(_ context.Context, _ *database.Tx, before value.Optional[Member], draft *MemberDraft) error {
			old, exists := before.Get()
			next, assigned := draft.Email().Get()
			if !exists || (assigned && next != old.Email) {
				revision, err := challenge.NewRevision[Member]()
				if err != nil {
					return err
				}
				*draft = draft.SetEmailRevision(revision).SetEmailVerified(false)
				return nil
			}
			if old.EmailRevision.IsZero() {
				return fault.New(fault.Invalid, "existing email state requires a revision backfill")
			}
			// Submitted draft values cannot restore an old generation.
			*draft = draft.UnsetEmailRevision()
			return nil
		},
		Saved: func(_ context.Context, _ *database.Tx, changes MemberChanges) error {
			current, exists := changes.After().Get()
			if !exists || current.EmailRevision.IsZero() {
				return fault.New(fault.Invalid, "saved email state has no revision")
			}
			if changes.Fields().Email.Changed() && (!changes.Fields().EmailRevision.Changed() || current.EmailVerified) {
				return fault.New(fault.Invalid, "email change requires fresh unverified state")
			}
			return nil
		},
	}
}
