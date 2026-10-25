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
		member, err := q.Insert(ctx, tx, draft...)
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
		createdAt, err := orm.ChangeOf(state.ORMChanges[0], q.CreatedAt)
		if err != nil {
			return err
		}
		createdUpdatedAt, err := orm.ChangeOf(state.ORMChanges[0], q.UpdatedAt)
		if err != nil {
			return err
		}
		if !createdAt.Assigned() || !createdUpdatedAt.Assigned() || !createdAt.Changed() || !createdUpdatedAt.Changed() {
			return errors.New("automatic timestamps were absent from effective assignments")
		}
		clock.source.Advance(time.Hour)
		originalCreated := member.CreatedAt
		state.ORMChanges = nil
		member, err = q.Update(ctx, tx, member.ID)
		if err != nil {
			return err
		}
		if !member.CreatedAt.Equal(originalCreated) || !member.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("timestamp-only update lost clock time or returned trigger values")
		}
		updatedAt, err := orm.ChangeOf(state.ORMChanges[0], q.UpdatedAt)
		if err != nil {
			return err
		}
		updatedCreatedAt, err := orm.ChangeOf(state.ORMChanges[0], q.CreatedAt)
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
		explicit, err := q.Insert(ctx, tx, timequeries.Members.Name.Set(" explicit "), timequeries.Members.CreatedAt.Set(old), timequeries.Members.UpdatedAt.Set(ignored))
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
		removed, err := q.Delete(ctx, tx, member.ID)
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
		member, err := q.Insert(t.Context(), tx, timequeries.Members.Name.Set("original"))
		if err != nil {
			return err
		}
		clock.source.Advance(time.Hour)
		veto := &timequeries.State{VetoAfter: true}
		if _, err := q.Update(timequeries.WithState(t.Context(), veto), tx, member.ID, timequeries.Members.Name.Set("changed")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp post-write veto did not abort")
		}
		current, err := q.Require(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt || len(veto.Committed) != 0 {
			return errors.New("timestamp veto changed stored data")
		}
		rollback := errors.New("outer savepoint veto")
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			clock.source.Advance(time.Hour)
			updated, err := q.Update(t.Context(), child, member.ID, timequeries.Members.Name.Set("nested"))
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
		current, err = q.Require(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
			return errors.New("outer savepoint did not roll back nested model time")
		}
		before := clock.calls.Load()
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := q.Update(canceled, tx, member.ID); !errors.Is(err, context.Canceled) || clock.calls.Load() != before {
			return errors.New("canceled timestamp write sampled time")
		}
		valid := clock.source.Now()
		clock.source.Set(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
		if _, err := q.Update(t.Context(), tx, member.ID); !errors.Is(err, fault.Invalid) {
			return errors.New("out-of-range clock time persisted")
		}
		clock.source.Set(valid)
		if _, err := q.Insert(t.Context(), tx, timequeries.Members.Name.Set("submicro"), timequeries.Members.CreatedAt.Set(valid)); !errors.Is(err, fault.Invalid) {
			return errors.New("manual sub-microsecond timestamp was silently truncated")
		}
		if _, err := q.Insert(t.Context(), tx, timequeries.Members.Name.Set("reject")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp convention bypassed a field mutator veto")
		}
		current, err = q.Require(t.Context(), tx, member.ID)
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
		members, err := q.InsertMany(ctx, tx, [][]orm.Assignment[timequeries.Member]{{timequeries.Members.Name.Set("first")}, {timequeries.Members.Name.Set("second")}})
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
		policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(orm.ConflictIncoming(f.Name))
		result, err := orm.Upsert(ctx, tx, q.Query(), policy, timequeries.Members.Key().Set(member.ID), timequeries.Members.Name.Set("updated"))
		if err != nil {
			return err
		}
		updated, ok := result.Get()
		if !ok || !updated.CreatedAt.Equal(member.CreatedAt) || !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("upsert did not retain creation time or normalized proposed update time")
		}
		clock.source.Advance(time.Hour)
		skipped, err := orm.Upsert(ctx, tx, q.Query(), orm.OnConflict(orm.ConflictKey(f.Key())).DoNothing(), timequeries.Members.Key().Set(member.ID), timequeries.Members.Name.Set("skip"))
		if err != nil || skipped.IsSet() {
			return errors.New("timestamp DO NOTHING changed conflict semantics")
		}
		current, err := q.Require(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.UpdatedAt != updated.UpdatedAt {
			return errors.New("skipped conflict changed stored timestamp")
		}
		before := clock.calls.Load()
		if _, err := q.InsertMany(ctx, tx, nil); err != nil {
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
		manual, err := timequeries.Manuals.Insert(t.Context(), tx, timequeries.Manuals.CreatedAt.Set(explicit), timequeries.Manuals.UpdatedAt.Set(explicitUpdated))
		if err != nil {
			return err
		}
		if !manual.CreatedAt.Equal(explicit) || manual.UpdatedAt != explicitUpdated || clock.calls.Load() != before {
			return errors.New("timestamps=false changed explicitly supplied fields")
		}
		if _, err := timequeries.Manuals.Update(t.Context(), tx, manual.ID); !errors.Is(err, fault.Invalid) {
			return errors.New("unmanaged empty update changed its contract")
		}
		if len(state.ORMChanges) != 0 || len(state.Committed) != 0 {
			return errors.New("bulk timestamp path invoked normal observers")
		}
		return nil
	})
}
