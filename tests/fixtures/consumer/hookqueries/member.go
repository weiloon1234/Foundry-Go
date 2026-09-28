// Package hookqueries exercises handwritten domain hooks through generated APIs.
package hookqueries

import (
	"context"
	"errors"
	"runtime"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=hook_members hooks=memberHooks
type Member struct {
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Email retains stored hook_members.email. Custom setter: [Member.MutateEmail] transforms assigned values during persistence through [MemberDraft.SetEmail]. Direct field assignment and draft construction do not invoke it.
	Email string
	// Foundry field behavior (generated): Member.Nickname retains stored hook_members.nickname. Custom setter: [Member.MutateNickname] transforms assigned values during persistence through [MemberDraft.SetNickname]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Nickname     value.Nullable[string]
	IntroducerID value.Nullable[model.ID[Member]]
	IsIntroducer bool `foundry:"default=database"`
	Rank         int  `foundry:"default=database"`
}

//foundry:model table=hook_logs
type Log struct {
	ID    model.ID[Log]
	Label string
}

var Veto = errors.New("injected hook veto")

func (Member) MutateEmail(v string) (string, error) {
	return strings.ToLower(strings.TrimSpace(v)), nil
}
func (Member) MutateNickname(v string) (string, error) { return v + "!", nil }

// Trace is fixture instrumentation supplied per operation through context,
// keeping tests independent without a mutable package-wide hook registry.
type Trace struct {
	Calls       []string
	Changes     []MemberChanges
	Operations  []lifecycle.Operation
	FailAt      string
	Failure     string
	Cancel      context.CancelFunc
	Recurse     bool
	AfterCommit func(context.Context, lifecycle.Operation, MemberChanges) error
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

func record(ctx context.Context, tx *database.Tx, name string, changes *MemberChanges) error {
	trace, _ := ctx.Value(traceKey{}).(*Trace)
	if trace != nil {
		trace.Calls = append(trace.Calls, name)
		if changes != nil {
			trace.Changes = append(trace.Changes, *changes)
		}
	}
	if tx != nil {
		if _, err := QueryHookLogs().Create(ctx, tx, LogDraft{}.SetLabel(name)); err != nil {
			return err
		}
	}
	if trace == nil || trace.FailAt != name {
		return nil
	}
	switch trace.Failure {
	case "panic":
		panic("private hook panic")
	case "goexit":
		runtime.Goexit()
	case "cancel":
		trace.Cancel()
		return nil
	}
	return Veto
}

func memberHooks() MemberHooks {
	// State belongs to this one write. Saving and Saved must share the same
	// factory instance; separate writes must never share this variable.
	savingCalled := false
	var trace *Trace
	return MemberHooks{
		Saving: func(ctx context.Context, tx *database.Tx, before value.Optional[Member], draft *MemberDraft) error {
			trace, _ = ctx.Value(traceKey{}).(*Trace)
			if savingCalled {
				return errors.New("factory reused across writes")
			}
			savingCalled = true
			return record(ctx, tx, "saving", nil)
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *MemberDraft) error {
			if err := record(ctx, tx, "creating", nil); err != nil {
				return err
			}
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil && trace.Recurse {
				_, err := QueryHookMembers().Create(ctx, tx, MemberDraft{})
				return err
			}
			if !draft.Email().IsSet() {
				*draft = draft.SetEmail(" DEFAULT@EXAMPLE.TEST ")
			}
			if !draft.IntroducerID().IsSet() {
				introducer, err := QueryHookMembers().Where(MemberFields().IsIntroducer.Eq(true)).RequireFirst(ctx, tx)
				if err != nil {
					return err
				}
				*draft = draft.SetIntroducerID(introducer.ID)
			}
			return nil
		},
		Updating: func(ctx context.Context, tx *database.Tx, before Member, draft *MemberDraft) error {
			if err := record(ctx, tx, "updating", nil); err != nil {
				return err
			}
			// A hook may supply an otherwise empty patch using the locked model.
			if draft.IsEmpty() {
				*draft = draft.SetRank(before.Rank + 1)
			}
			return nil
		},
		Deleting: func(ctx context.Context, tx *database.Tx, before Member) error {
			trace, _ = ctx.Value(traceKey{}).(*Trace)
			return record(ctx, tx, "deleting", nil)
		},
		Created: func(ctx context.Context, tx *database.Tx, changes MemberChanges) error {
			return record(ctx, tx, "created", &changes)
		},
		Updated: func(ctx context.Context, tx *database.Tx, changes MemberChanges) error {
			return record(ctx, tx, "updated", &changes)
		},
		Deleted: func(ctx context.Context, tx *database.Tx, changes MemberChanges) error {
			return record(ctx, tx, "deleted", &changes)
		},
		Saved: func(ctx context.Context, tx *database.Tx, changes MemberChanges) error {
			if !savingCalled {
				return errors.New("hook factory was recreated mid-write")
			}
			return record(ctx, tx, "saved", nil)
		},
		AfterCommit: func(ctx context.Context, operation lifecycle.Operation, changes MemberChanges) error {
			ctx = WithTrace(ctx, trace)
			if err := record(ctx, nil, "committed", nil); err != nil {
				return err
			}
			if trace, _ := ctx.Value(traceKey{}).(*Trace); trace != nil {
				trace.Operations = append(trace.Operations, operation)
				if trace.AfterCommit != nil {
					return trace.AfterCommit(ctx, operation, changes)
				}
			}
			return nil
		},
	}
}
