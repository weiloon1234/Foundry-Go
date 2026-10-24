package observerqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RecordTable declares the mutating field and per-write local hooks. Example: Records.Name.Set("new").
type RecordTable struct {
	orm.Table[Record, model.ID[Record]]
	Name orm.InputText[Record, string, string]
}

// Records dispatches local hooks before registered ORM observers. Example: Records.Insert(ctx,db,Records.Name.Set("new")).
var Records = declareRecords()

func declareRecords() RecordTable {
	var fields RecordTable
	fields = orm.Define("observed_records", func(b orm.Builder[Record], m *Record) RecordTable {
		return RecordTable{orm.TableOf(b, &m.ID), orm.InputTextOf[Record, string](b, &m.Name, nil)}
	}, orm.WithHooks(func() orm.Hooks[Record] { return observerHooksFor("local", fields) }))
	return fields
}

// PlainTable has no local hook factory. Example: Plains.Name.Eq("stored").
type PlainTable struct {
	orm.Table[Plain, model.ID[Plain]]
	Name orm.Text[Plain, string]
}

// Plains still dispatches application-owned observers. Example: Plains.Insert(ctx,db).
var Plains = orm.Define("observed_plain", func(b orm.Builder[Plain], m *Plain) PlainTable {
	return PlainTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
})

// EffectTable declares transactional observer side effects. Example: Effects.Label.Set("saved").
type EffectTable struct {
	orm.Table[Effect, model.ID[Effect]]
	Label orm.Text[Effect, string]
}

// Effects uses the caller's transaction. Example: Effects.Insert(ctx,tx,Effects.Label.Set("saved")).
var Effects = orm.Define("observed_effects", func(b orm.Builder[Effect], m *Effect) EffectTable {
	return EffectTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Label)}
})

// ORMObserverHooks preserves operation-local draft and change capture.
// Register only this adapter family when using Records; example: orm.NewObserver[Record]("first").Declare(func() orm.Hooks[Record] { return ORMObserverHooks("first") }).
func ORMObserverHooks(name string) orm.Hooks[Record] {
	return observerHooksFor(name, Records)
}

func observerHooksFor(name string, fields RecordTable) orm.Hooks[Record] {
	var trace *Trace
	savingCalled := false
	before := func(ctx context.Context, tx *database.Tx, stage string, draft *orm.Write[Record]) error {
		trace = traceOf(ctx)
		if err := record(ctx, tx, trace, name+"."+stage); err != nil {
			return err
		}
		current, set := orm.WriteGet(draft, fields.Name.Set).Get()
		if !set {
			current = "anonymous"
		}
		return orm.WriteSet(draft, fields.Name.Set(current+"|"+name+"."+stage))
	}
	after := func(ctx context.Context, tx *database.Tx, stage string, changes orm.Changes[Record]) error {
		if trace != nil {
			trace.ORMChanges = append(trace.ORMChanges, changes)
		}
		return record(ctx, tx, trace, name+"."+stage)
	}
	return orm.Hooks[Record]{
		Saving: func(ctx context.Context, tx *database.Tx, _ value.Optional[Record], draft *orm.Write[Record]) error {
			if savingCalled {
				return errors.New("observer factory reused across operations")
			}
			savingCalled = true
			return before(ctx, tx, "saving", draft)
		},
		Creating: func(ctx context.Context, tx *database.Tx, draft *orm.Write[Record]) error {
			return before(ctx, tx, "creating", draft)
		},
		Updating: func(ctx context.Context, tx *database.Tx, _ Record, draft *orm.Write[Record]) error {
			return before(ctx, tx, "updating", draft)
		},
		Deleting: func(ctx context.Context, tx *database.Tx, _ Record) error {
			trace = traceOf(ctx)
			return record(ctx, tx, trace, name+".deleting")
		},
		Created: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "created", c)
		},
		Updated: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "updated", c)
		},
		Deleted: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			return after(ctx, tx, "deleted", c)
		},
		Saved: func(ctx context.Context, tx *database.Tx, c orm.Changes[Record]) error {
			if !savingCalled {
				return errors.New("observer recreated between saving and saved")
			}
			return after(ctx, tx, "saved", c)
		},
		AfterCommit: func(ctx context.Context, _ lifecycle.Operation, _ orm.Changes[Record]) error {
			return record(ctx, nil, trace, name+".committed")
		},
	}
}
