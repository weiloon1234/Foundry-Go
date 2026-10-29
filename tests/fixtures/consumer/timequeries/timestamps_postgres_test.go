package timequeries_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/timequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestPostgresManagedTimestampsAndHookChanges(t *testing.T) {
	state := &timequeries.State{}
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		ctx := timequeries.WithState(t.Context(), state)
		q := timequeries.Members
		draft := []orm.Assignment[timequeries.Member]{}
		member, err := orm.Insert(ctx, tx, q.Table, draft...)
		if err != nil {
			return err
		}
		if member.Name != "default" || !member.CreatedAt.Equal(clock.stored()) || !member.UpdatedAt.UTC().Equal(clock.stored()) || clock.calls.Load() != 1 {
			return errors.New("create lost automatic defaults or its one clock sample")
		}
		if len(draft) != 0 || state.ORMCreatedAt.IsSet() || state.ORMUpdatedAt.IsSet() {
			return errors.New("timestamps changed caller input or ran before the hooks")
		}
		if len(state.ORMChanges) != 1 {
			return errors.New("create did not capture timestamp changes")
		}
		createdAt, err := orm.ChangeOf(state.ORMChanges[0], orm.TimeExpr(q.CreatedAt))
		if err != nil {
			return err
		}
		createdUpdatedAt, err := orm.ChangeOf(state.ORMChanges[0], orm.TimeExpr(q.UpdatedAt))
		if err != nil {
			return err
		}
		if !createdAt.Assigned() || !createdUpdatedAt.Assigned() || !createdAt.Changed() || !createdUpdatedAt.Changed() {
			return errors.New("automatic timestamps were absent from effective assignments")
		}
		clock.source.Advance(time.Hour)
		originalCreated := member.CreatedAt
		state.ORMChanges = nil
		member, err = orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID)
		if err != nil {
			return err
		}
		if !member.CreatedAt.Equal(originalCreated) || !member.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("timestamp-only update lost clock time or returned trigger values")
		}
		updatedAt, err := orm.ChangeOf(state.ORMChanges[0], orm.TimeExpr(q.UpdatedAt))
		if err != nil {
			return err
		}
		updatedCreatedAt, err := orm.ChangeOf(state.ORMChanges[0], orm.TimeExpr(q.CreatedAt))
		if err != nil {
			return err
		}
		after, ok := updatedAt.After().Get()
		if !ok || after != member.UpdatedAt || !updatedAt.Assigned() || !updatedAt.Changed() || updatedCreatedAt.Assigned() || updatedCreatedAt.Changed() {
			return errors.New("update change set did not use stored trigger output")
		}
		old := clock.stored().Add(-24 * time.Hour)
		ignored, _ := temporal.NewDateTime(old)
		state.ORMChanges = nil
		explicit, err := orm.Insert(ctx, tx, q.Table, orm.InputTextSet(timequeries.Members.Name, " explicit "), orm.TimeSet(timequeries.Members.CreatedAt, old), orm.TimeSet(timequeries.Members.UpdatedAt, ignored))
		if err != nil {
			return err
		}
		if !explicit.CreatedAt.Equal(old) || !explicit.UpdatedAt.UTC().Equal(clock.stored()) {
			return errors.New("explicit creation time or managed update time changed")
		}
		if input, ok := state.ORMUpdatedAt.Get(); !ok || input != ignored {
			return errors.New("before hook lost explicit timestamp input")
		}
		beforeDelete := clock.calls.Load()
		removed, err := orm.Delete(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID)
		if err != nil {
			return err
		}
		if removed.UpdatedAt != member.UpdatedAt || clock.calls.Load() != beforeDelete {
			return errors.New("physical deletion applied timestamp conventions")
		}
		if len(state.Committed) != 0 {
			return errors.New("timestamp write committed before its outer transaction")
		}
		return nil
	})
	if len(state.Committed) != 4 || state.Committed[0] != lifecycle.Create || state.Committed[1] != lifecycle.Update || state.Committed[2] != lifecycle.Create || state.Committed[3] != lifecycle.Delete {
		t.Fatal("timestamp writes lost outer after-commit order", state.Committed)
	}
}

func TestPostgresTimestampFailuresAndNestedClockOwnership(t *testing.T) {
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		q := timequeries.Members
		member, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(timequeries.Members.Name, "original"))
		if err != nil {
			return err
		}
		clock.source.Advance(time.Hour)
		veto := &timequeries.State{VetoAfter: true}
		if _, err := orm.Update(timequeries.WithState(t.Context(), veto), tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(timequeries.Members.Name, "changed")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp post-write veto did not abort")
		}
		current, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt || len(veto.Committed) != 0 {
			return errors.New("timestamp veto changed stored data")
		}
		rollback := errors.New("outer savepoint veto")
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			clock.source.Advance(time.Hour)
			updated, err := orm.Update(t.Context(), child, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(timequeries.Members.Name, "nested"))
			if err != nil {
				return err
			}
			if !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2 * time.Second)) {
				return errors.New("nested write lost the owning application clock")
			}
			return rollback
		}); !errors.Is(err, rollback) {
			if err == nil {
				return errors.New("outer savepoint unexpectedly committed")
			}
			return err
		}
		current, err = orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
			return errors.New("outer savepoint did not roll back nested model time")
		}
		before := clock.calls.Load()
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := orm.Update(canceled, tx, q.Table.Query(), q.Table.Key(), member.ID); !errors.Is(err, context.Canceled) || clock.calls.Load() != before {
			return errors.New("canceled timestamp write sampled time")
		}
		valid := clock.source.Now()
		clock.source.Set(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
		if _, err := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID); !errors.Is(err, fault.Invalid) {
			return errors.New("out-of-range clock time persisted")
		}
		clock.source.Set(valid)
		if _, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(timequeries.Members.Name, "submicro"), orm.TimeSet(timequeries.Members.CreatedAt, valid)); !errors.Is(err, fault.Invalid) {
			return errors.New("manual sub-microsecond timestamp was silently truncated")
		}
		if _, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(timequeries.Members.Name, "reject")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp convention bypassed a field mutator veto")
		}
		current, err = orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
			return errors.New("failed timestamp writes damaged their parent scope")
		}
		return nil
	})
}

func TestPostgresTimestampBulkUpsertAndOptOut(t *testing.T) {
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		state := &timequeries.State{VetoAfter: true}
		ctx := timequeries.WithState(t.Context(), state)
		q, f := timequeries.Members, timequeries.Members
		members, err := orm.InsertMany(ctx, tx, q.Table, [][]orm.Assignment[timequeries.Member]{{orm.InputTextSet(timequeries.Members.Name, "first")}, {orm.InputTextSet(timequeries.Members.Name, "second")}})
		if err != nil {
			return err
		}
		if len(members) != 2 || clock.calls.Load() != 1 || len(state.ORMChanges) != 0 {
			return errors.New("bulk timestamps ran per row or dispatched observers")
		}
		for _, member := range members {
			if !member.CreatedAt.Equal(clock.stored()) || !member.UpdatedAt.UTC().Equal(clock.stored()) {
				return errors.New("bulk records did not share a timestamp")
			}
		}
		member := members[0]
		clock.source.Advance(time.Hour)
		policy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(orm.ConflictIncoming(orm.InputTextExpr(f.Name)))
		result, err := orm.Upsert(ctx, tx, q.Query(), policy, orm.KeySet(timequeries.Members.Key(), member.ID), orm.InputTextSet(timequeries.Members.Name, "updated"))
		if err != nil {
			return err
		}
		updated, ok := result.Get()
		if !ok || !updated.CreatedAt.Equal(member.CreatedAt) || !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("upsert did not retain creation time or normalized proposed update time")
		}
		clock.source.Advance(time.Hour)
		skipped, err := orm.Upsert(ctx, tx, q.Query(), orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoNothing(), orm.KeySet(timequeries.Members.Key(), member.ID), orm.InputTextSet(timequeries.Members.Name, "skip"))
		if err != nil || skipped.IsSet() {
			return errors.New("timestamp DO NOTHING changed conflict semantics")
		}
		current, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if current.UpdatedAt != updated.UpdatedAt {
			return errors.New("skipped conflict changed stored timestamp")
		}
		before := clock.calls.Load()
		if _, err := orm.InsertMany(ctx, tx, q.Table, nil); err != nil {
			return err
		}
		if _, err := orm.UpsertMany(ctx, tx, q.Query(), policy, nil); err != nil {
			return err
		}
		if clock.calls.Load() != before {
			return errors.New("empty batch sampled application time")
		}
		explicit := clock.stored().Add(-24 * time.Hour)
		explicitUpdated, _ := temporal.NewDateTime(explicit.Add(time.Hour))
		manual, err := orm.Insert(t.Context(), tx, timequeries.Manuals.Table, orm.TimeSet(timequeries.Manuals.CreatedAt, explicit), orm.TimeSet(timequeries.Manuals.UpdatedAt, explicitUpdated))
		if err != nil {
			return err
		}
		if !manual.CreatedAt.Equal(explicit) || manual.UpdatedAt != explicitUpdated || clock.calls.Load() != before {
			return errors.New("timestamps=false changed explicitly supplied fields")
		}
		if _, err := orm.Update(t.Context(), tx, timequeries.Manuals.Table.Query(), timequeries.Manuals.Table.Key(), manual.ID); !errors.Is(err, fault.Invalid) {
			return errors.New("unmanaged empty update changed its contract")
		}
		if len(state.ORMChanges) != 0 || len(state.Committed) != 0 {
			return errors.New("bulk timestamp path invoked normal observers")
		}
		return nil
	})
}
