package hooks

import (
	"context"
	"time"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=hook_members primary=ID hooks=LocalHooks
type Member struct {
	ID int64
	// Foundry field behavior (generated): Member.Name retains stored hook_members.name. Custom setter: [Member.MutateName] transforms assigned values during persistence through [MemberDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name string `foundry:"default=database"`
	// Foundry field behavior (generated): Member.Note retains stored hook_members.note. Custom setter: [Member.MutateNote] transforms assigned values during persistence through [MemberDraft.SetNote]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts coremodels.LabelInput and produces stored coremodels.StoredLabel; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Note value.Nullable[coremodels.StoredLabel]
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	Body []byte
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt time.Time
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[time.Time]
	Scratch   string `foundry:"-"`
}

func (Member) MutateName(v string) (string, error) { return coremodels.NormalizeName(v) }
func (Member) MutateNote(v coremodels.LabelInput) (coremodels.StoredLabel, error) {
	return coremodels.NormalizeLabel(v)
}
func LocalHooks() MemberHooks { return Hooks("local") }
func Hooks(prefix string) MemberHooks {
	trace := func(ctx context.Context, stage string) { coremodels.TraceLifecycle(ctx, prefix+"."+stage) }
	before := func(stage string) func(context.Context, *database.Tx, Member) error {
		return func(ctx context.Context, tx *database.Tx, m Member) error { trace(ctx, stage); return nil }
	}
	after := func(stage string) func(context.Context, *database.Tx, MemberChanges) error {
		return func(ctx context.Context, tx *database.Tx, c MemberChanges) error { trace(ctx, stage); return nil }
	}
	return MemberHooks{
		Saving: func(ctx context.Context, tx *database.Tx, original value.Optional[Member], w *MemberDraft) error {
			trace(ctx, "saving")
			return nil
		},
		Creating: func(ctx context.Context, tx *database.Tx, w *MemberDraft) error { trace(ctx, "creating"); return nil },
		Updating: func(ctx context.Context, tx *database.Tx, m Member, w *MemberDraft) error {
			trace(ctx, "updating")
			return nil
		},
		Deleting: before("deleting"), Restoring: before("restoring"), ForceDeleting: before("force_deleting"),
		Created: after("created"), Updated: after("updated"), Deleted: after("deleted"), Saved: after("saved"), Restored: after("restored"), ForceDeleted: after("force_deleted"),
		AfterCommit: func(ctx context.Context, op lifecycle.Operation, c MemberChanges) error {
			trace(ctx, "commit."+op.String())
			return nil
		},
	}
}
