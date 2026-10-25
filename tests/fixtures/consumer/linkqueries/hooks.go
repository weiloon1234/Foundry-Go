package linkqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var ErrVeto = errors.New("relation fixture veto")

type Trace struct {
	InspectArchive    func(*orm.Write[Archive]) error
	InspectMembership func(*orm.Write[Membership]) error
	Steps             []string
	ORMChanges        []orm.Changes[Membership]
	Committed         []lifecycle.Operation
	Reads             int
	VetoAt            string
	VetoDeletion      int
	Deletions         int
	CancelAt          string
	Cancel            context.CancelFunc
	OnPhase           func(context.Context, string) error
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

func membershipOperation(owner string) (func(context.Context, string) error, func(lifecycle.Operation)) {
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
	return before, func(operation lifecycle.Operation) {
		if captured != nil {
			captured.Committed = append(captured.Committed, operation)
		}
	}
}
