package linkqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

var ErrVeto = errors.New("relation fixture veto")

type Trace struct {
	Steps        []string
	Changes      []MembershipChanges
	Committed    []lifecycle.Operation
	Reads        int
	VetoAt       string
	VetoDeletion int
	Deletions    int
	CancelAt     string
	Cancel       context.CancelFunc
	OnPhase      func(context.Context, string) error
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
func traceFrom(ctx context.Context) *Trace { trace, _ := ctx.Value(traceKey{}).(*Trace); return trace }
func recordRead(ctx context.Context) {
	if trace := traceFrom(ctx); trace != nil {
		trace.Reads++
	}
}
func membershipHooks() MembershipHooks { return Hooks("local") }

func Hooks(owner string) MembershipHooks {
	var captured *Trace
	before := func(ctx context.Context, name string) error {
		captured = traceFrom(ctx)
		if captured == nil {
			return nil
		}
		phase := owner + "." + name
		captured.Steps = append(captured.Steps, phase)
		if phase == "local.deleting" {
			captured.Deletions++
			if captured.VetoDeletion == captured.Deletions {
				return ErrVeto
			}
		}
		if captured.VetoAt == phase {
			return ErrVeto
		}
		if captured.CancelAt == phase && captured.Cancel != nil {
			captured.Cancel()
		}
		if captured.OnPhase != nil {
			return captured.OnPhase(ctx, phase)
		}
		return nil
	}
	after := func(ctx context.Context, name string, changes MembershipChanges) error {
		if trace := traceFrom(ctx); trace != nil {
			trace.Changes = append(trace.Changes, changes)
		}
		return before(ctx, name)
	}
	return MembershipHooks{
		Saving: func(ctx context.Context, _ *database.Tx, prior value.Optional[Membership], draft *MembershipDraft) error {
			if !prior.IsSet() && (!draft.MemberID().IsSet() || !draft.GroupCode().IsSet()) {
				return errors.New("pivot hooks did not receive derived keys")
			}
			return before(ctx, "saving")
		},
		Creating:      func(ctx context.Context, _ *database.Tx, _ *MembershipDraft) error { return before(ctx, "creating") },
		Deleting:      func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "deleting") },
		Restoring:     func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "restoring") },
		ForceDeleting: func(ctx context.Context, _ *database.Tx, _ Membership) error { return before(ctx, "force-deleting") },
		Created:       func(ctx context.Context, _ *database.Tx, c MembershipChanges) error { return after(ctx, "created", c) },
		Saved:         func(ctx context.Context, _ *database.Tx, c MembershipChanges) error { return after(ctx, "saved", c) },
		Deleted:       func(ctx context.Context, _ *database.Tx, c MembershipChanges) error { return after(ctx, "deleted", c) },
		Restored:      func(ctx context.Context, _ *database.Tx, c MembershipChanges) error { return after(ctx, "restored", c) },
		ForceDeleted: func(ctx context.Context, _ *database.Tx, c MembershipChanges) error {
			return after(ctx, "force-deleted", c)
		},
		AfterCommit: func(_ context.Context, operation lifecycle.Operation, _ MembershipChanges) error {
			if captured != nil {
				captured.Committed = append(captured.Committed, operation)
			}
			return nil
		},
	}
}
