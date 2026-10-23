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

// Member declares lifecycle parity independently of generated model adapters.
type Member struct {
	ID        int64
	Name      string `foundry:"default=database"`
	Note      value.Nullable[coremodels.StoredLabel]
	Body      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
	Scratch   string `foundry:"-"`
}

func (Member) MutateName(v string) (string, error) { return coremodels.NormalizeName(v) }
func (Member) MutateNote(v coremodels.LabelInput) (coremodels.StoredLabel, error) {
	return coremodels.NormalizeLabel(v)
}

type MemberTable struct {
	orm.Table[Member, int64]
	Name      orm.Text[Member, string]
	Note      orm.NullableInputText[Member, coremodels.LabelInput, coremodels.StoredLabel]
	Body      orm.Column[Member, []byte]
	CreatedAt orm.Time[Member, time.Time]
	UpdatedAt orm.Time[Member, time.Time]
	DeletedAt orm.NullableTime[Member, time.Time]
}

var Members = Declare(LocalHooks)

// Declare accepts an immutable per-write factory; runtime state belongs to its caller.
func Declare(factory func() orm.Hooks[Member]) MemberTable {
	return orm.Define("hook_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), orm.NullableInputTextOf[Member, coremodels.LabelInput](b, &m.Note, nil), orm.BytesOf(b, &m.Body), orm.TimeOf(b, &m.CreatedAt), orm.TimeOf(b, &m.UpdatedAt), orm.NullableTimeOf(b, &m.DeletedAt)}
	}, orm.WithHooks(factory))
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
