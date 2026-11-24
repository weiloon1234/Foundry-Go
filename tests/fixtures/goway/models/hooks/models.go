package hooks

import (
	"context"
	"time"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Member declares lifecycle parity with operation-local hook state.
//
//foundry:model table=hook_members var=Members key=ID hooks=LocalHooks identity=false
type Member struct {
	ID        int64                                  `foundry:"member=ID,key-role=primary"`
	Name      string                                 `foundry:"default=database,input=string"`
	Note      value.Nullable[coremodels.StoredLabel] `foundry:"input=coremodels.LabelInput"`
	Body      []byte
	CreatedAt time.Time                 `foundry:"role=created-at"`
	UpdatedAt time.Time                 `foundry:"role=updated-at"`
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
	Scratch   string                    `foundry:"-"`
}

func (Member) MutateName(v string) (string, error) { return coremodels.NormalizeName(v) }
func (Member) MutateNote(v coremodels.LabelInput) (coremodels.StoredLabel, error) {
	return coremodels.NormalizeLabel(v)
}

// LocalHooks declares every old write stage; callbacks use the operation's trace only.
func LocalHooks() orm.Hooks[Member] { return Hooks("local") }

// Hooks constructs an operation-local observer or local declaration.
func Hooks(prefix string) orm.Hooks[Member] {
	trace := func(ctx context.Context, stage string) { coremodels.TraceLifecycle(ctx, prefix+"."+stage) }
	before := func(stage string) func(context.Context, *database.Tx, Member) error {
		return func(ctx context.Context, tx *database.Tx, m Member) error { trace(ctx, stage); return nil }
	}
	after := func(stage string) func(context.Context, *database.Tx, orm.Changes[Member]) error {
		return func(ctx context.Context, tx *database.Tx, c orm.Changes[Member]) error { trace(ctx, stage); return nil }
	}
	return orm.Hooks[Member]{
		Saving: func(ctx context.Context, tx *database.Tx, original value.Optional[Member], w *orm.Write[Member]) error {
			trace(ctx, "saving")
			return nil
		},
		Creating: func(ctx context.Context, tx *database.Tx, w *orm.Write[Member]) error {
			trace(ctx, "creating")
			return nil
		},
		Updating: func(ctx context.Context, tx *database.Tx, m Member, w *orm.Write[Member]) error {
			trace(ctx, "updating")
			return nil
		},
		Deleting: before("deleting"), Restoring: before("restoring"), ForceDeleting: before("force_deleting"),
		Created: after("created"), Updated: after("updated"), Deleted: after("deleted"), Saved: after("saved"), Restored: after("restored"), ForceDeleted: after("force_deleted"),
		AfterCommit: func(ctx context.Context, op lifecycle.Operation, c orm.Changes[Member]) error {
			trace(ctx, "commit."+op.String())
			return nil
		},
	}
}
