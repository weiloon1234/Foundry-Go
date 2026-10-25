package recovering

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// The revision belongs to the email generation, never to submitted draft values.
func nextEmailRevision(before value.Optional[Member], email value.Optional[string]) (value.Optional[challenge.Revision[Member]], error) {
	old, exists := before.Get()
	next, assigned := email.Get()
	if !exists || (assigned && next != old.Email) {
		revision, err := challenge.NewRevision[Member]()
		return value.Set(revision), err
	}
	if old.EmailRevision.IsZero() {
		return value.Optional[challenge.Revision[Member]]{}, fault.New(fault.Invalid, "existing email state requires a revision backfill")
	}
	return value.Optional[challenge.Revision[Member]]{}, nil
}

func checkEmailState(after value.Optional[Member], emailChanged, revisionChanged bool) error {
	current, exists := after.Get()
	if !exists || current.EmailRevision.IsZero() {
		return fault.New(fault.Invalid, "saved email state has no revision")
	}
	if emailChanged && (!revisionChanged || current.EmailVerified) {
		return fault.New(fault.Invalid, "email change requires fresh unverified state")
	}
	return nil
}

func ormMemberHooks(fields MemberTable) orm.Hooks[Member] {
	return orm.Hooks[Member]{
		Saving: func(_ context.Context, _ *database.Tx, before value.Optional[Member], draft *orm.Write[Member]) error {
			revision, err := nextEmailRevision(before, orm.WriteGet(draft, fields.Email.Set))
			if err != nil {
				return err
			}
			if next, ok := revision.Get(); ok {
				return orm.WriteSet(draft, fields.EmailRevision.Set(next), fields.EmailVerified.Set(false))
			}
			return orm.WriteUnset(draft, fields.EmailRevision.Set)
		},
		Saved: func(_ context.Context, _ *database.Tx, changes orm.Changes[Member]) error {
			email, err := orm.ChangeOf(changes, fields.Email)
			if err != nil {
				return err
			}
			revision, err := orm.ChangeOf(changes, fields.EmailRevision)
			if err != nil {
				return err
			}
			return checkEmailState(changes.After(), email.Changed(), revision.Changed())
		},
	}
}

// memberHooks retains the generator's callback contract until its retirement.
// Both adapters use the same revision policy; consumers register only the ORM one.
func memberHooks() MemberHooks {
	return MemberHooks{
		Saving: func(_ context.Context, _ *database.Tx, before value.Optional[Member], draft *MemberDraft) error {
			revision, err := nextEmailRevision(before, draft.Email())
			if err != nil {
				return err
			}
			if next, ok := revision.Get(); ok {
				*draft = draft.SetEmailRevision(next).SetEmailVerified(false)
			} else {
				*draft = draft.UnsetEmailRevision()
			}
			return nil
		},
		Saved: func(_ context.Context, _ *database.Tx, changes MemberChanges) error {
			return checkEmailState(changes.After(), changes.Fields().Email.Changed(), changes.Fields().EmailRevision.Changed())
		},
	}
}
