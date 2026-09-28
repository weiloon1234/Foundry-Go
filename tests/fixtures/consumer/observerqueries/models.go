// Package observerqueries verifies provider observers through generated writes.
package observerqueries

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=observed_records hooks=localHooks
type Record struct {
	ID model.ID[Record]
	// Foundry field behavior (generated): Record.Name retains stored observed_records.name. Custom setter: [Record.MutateName] transforms assigned values during persistence through [RecordDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name string
}

//foundry:model table=observed_plain
type Plain struct {
	ID   model.ID[Plain]
	Name string
}

//foundry:model table=observed_effects
type Effect struct {
	ID    model.ID[Effect]
	Label string
}

func (Record) MutateName(name string) (string, error) {
	return strings.ToLower(strings.TrimSpace(name)) + "!", nil
}

var Veto = errors.New("observer veto")

type Trace struct {
	Calls   []string
	Changes []RecordChanges
	FailAt  string
	Mode    string
	Cancel  context.CancelFunc
}
type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
func traceOf(ctx context.Context) *Trace { trace, _ := ctx.Value(traceKey{}).(*Trace); return trace }

// Stats belongs to the application fixture; factories may run concurrently.
type Stats struct {
	First     atomic.Int64
	Second    atomic.Int64
	Plain     atomic.Int64
	Retrieval atomic.Int64
}

func record(ctx context.Context, tx *database.Tx, trace *Trace, label string) error {
	if trace != nil {
		trace.Calls = append(trace.Calls, label)
	}
	if tx != nil {
		if _, err := QueryObservedEffects().Create(ctx, tx, EffectDraft{}.SetLabel(label)); err != nil {
			return err
		}
	}
	if trace == nil || trace.FailAt != label {
		return nil
	}
	switch trace.Mode {
	case "panic":
		panic("observer panic")
	case "goexit":
		runtime.Goexit()
	case "cancel":
		trace.Cancel()
		return nil
	}
	return Veto
}

func localHooks() RecordHooks { return ObserverHooks("local") }

// ObserverHooks uses operation-local state across all callback stages.
func ObserverHooks(name string) RecordHooks {
	var trace *Trace
	savingCalled := false
	before := func(ctx context.Context, tx *database.Tx, stage string, draft *RecordDraft) error {
		trace = traceOf(ctx)
		if err := record(ctx, tx, trace, name+"."+stage); err != nil {
			return err
		}
		current, set := draft.Name().Get()
		if !set {
			current = "anonymous"
		}
		*draft = draft.SetName(current + "|" + name + "." + stage)
		return nil
	}
	after := func(ctx context.Context, tx *database.Tx, stage string, changes RecordChanges) error {
		if trace != nil {
			trace.Changes = append(trace.Changes, changes)
		}
		return record(ctx, tx, trace, name+"."+stage)
	}
	return RecordHooks{
		Saving: func(ctx context.Context, tx *database.Tx, _ value.Optional[Record], draft *RecordDraft) error {
			if savingCalled {
				return errors.New("observer factory reused across operations")
			}
			savingCalled = true
			return before(ctx, tx, "saving", draft)
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *RecordDraft) error {
			return before(ctx, tx, "creating", draft)
		},
		Updating: func(ctx context.Context, tx *database.Tx, _ Record, draft *RecordDraft) error {
			return before(ctx, tx, "updating", draft)
		},
		Deleting: func(ctx context.Context, tx *database.Tx, _ Record) error {
			trace = traceOf(ctx)
			return record(ctx, tx, trace, name+".deleting")
		},
		Created: func(ctx context.Context, tx *database.Tx, changes RecordChanges) error {
			return after(ctx, tx, "created", changes)
		},
		Updated: func(ctx context.Context, tx *database.Tx, changes RecordChanges) error {
			return after(ctx, tx, "updated", changes)
		},
		Deleted: func(ctx context.Context, tx *database.Tx, changes RecordChanges) error {
			return after(ctx, tx, "deleted", changes)
		},
		Saved: func(ctx context.Context, tx *database.Tx, changes RecordChanges) error {
			if !savingCalled {
				return errors.New("observer recreated between saving and saved")
			}
			return after(ctx, tx, "saved", changes)
		},
		AfterCommit: func(ctx context.Context, _ lifecycle.Operation, _ RecordChanges) error {
			return record(ctx, nil, trace, name+".committed")
		},
	}
}
