package timequeries_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/timequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestPostgresManagedTimestampsAndHookChanges(t *testing.T) {
	state := &timequeries.State{}
	runTimed(t, func(tx *database.Tx, clock *countingClock) error {
		ctx := timequeries.WithState(t.Context(), state)
		q := timequeries.QueryTimeMembers()
		draft := timequeries.MemberDraft{}
		member, err := q.Create(ctx, tx, draft)
		if err != nil {
			return err
		}
		if member.Name != "default" || !member.CreatedAt.Equal(clock.stored()) || !member.UpdatedAt.UTC().Equal(clock.stored()) || clock.calls.Load() != 1 {
			return errors.New("create lost automatic defaults or its one clock sample")
		}
		if !draft.IsEmpty() || state.Before.CreatedAt().IsSet() || state.Before.UpdatedAt().IsSet() {
			return errors.New("timestamps changed caller input or ran before the hooks")
		}
		if len(state.Changes) != 1 {
			return errors.New("create did not capture timestamp changes")
		}
		created := state.Changes[0].Fields()
		if !created.CreatedAt.Assigned() || !created.UpdatedAt.Assigned() || !created.CreatedAt.Changed() || !created.UpdatedAt.Changed() {
			return errors.New("automatic timestamps were absent from effective assignments")
		}
		clock.source.Advance(time.Hour)
		originalCreated := member.CreatedAt
		state.Changes = nil
		member, err = q.Update(ctx, tx, member.ID, timequeries.MemberDraft{})
		if err != nil {
			return err
		}
		if !member.CreatedAt.Equal(originalCreated) || !member.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("timestamp-only update lost clock time or returned trigger values")
		}
		fields := state.Changes[0].Fields()
		after, ok := fields.UpdatedAt.After().Get()
		if !ok || after != member.UpdatedAt || !fields.UpdatedAt.Assigned() || !fields.UpdatedAt.Changed() || fields.CreatedAt.Assigned() || fields.CreatedAt.Changed() {
			return errors.New("update change set did not use stored trigger output")
		}
		old := clock.stored().Add(-24 * time.Hour)
		ignored, _ := temporal.NewDateTime(old)
		state.Changes = nil
		explicit, err := q.Create(ctx, tx, timequeries.MemberDraft{}.SetName(" explicit ").SetCreatedAt(old).SetUpdatedAt(ignored))
		if err != nil {
			return err
		}
		if !explicit.CreatedAt.Equal(old) || !explicit.UpdatedAt.UTC().Equal(clock.stored()) {
			return errors.New("explicit creation time or managed update time changed")
		}
		if input, ok := state.Before.UpdatedAt().Get(); !ok || input != ignored {
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
		q := timequeries.QueryTimeMembers()
		member, err := q.Create(t.Context(), tx, timequeries.MemberDraft{}.SetName("original"))
		if err != nil {
			return err
		}
		clock.source.Advance(time.Hour)
		veto := &timequeries.State{VetoAfter: true}
		if _, err := q.Update(timequeries.WithState(t.Context(), veto), tx, member.ID, timequeries.MemberDraft{}.SetName("changed")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp post-write veto did not abort")
		}
		current, err := q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt || len(veto.Committed) != 0 {
			return errors.New("timestamp veto changed stored data")
		}
		rollback := errors.New("outer savepoint veto")
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			clock.source.Advance(time.Hour)
			updated, err := q.Update(t.Context(), child, member.ID, timequeries.MemberDraft{}.SetName("nested"))
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
		current, err = q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.Name != member.Name || current.UpdatedAt != member.UpdatedAt {
			return errors.New("outer savepoint did not roll back nested model time")
		}
		before := clock.calls.Load()
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := q.Update(canceled, tx, member.ID, timequeries.MemberDraft{}); !errors.Is(err, context.Canceled) || clock.calls.Load() != before {
			return errors.New("canceled timestamp write sampled time")
		}
		valid := clock.source.Now()
		clock.source.Set(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
		if _, err := q.Update(t.Context(), tx, member.ID, timequeries.MemberDraft{}); !errors.Is(err, fault.Invalid) {
			return errors.New("out-of-range clock time persisted")
		}
		clock.source.Set(valid)
		if _, err := q.Create(t.Context(), tx, timequeries.MemberDraft{}.SetName("submicro").SetCreatedAt(valid)); !errors.Is(err, fault.Invalid) {
			return errors.New("manual sub-microsecond timestamp was silently truncated")
		}
		if _, err := q.Create(t.Context(), tx, timequeries.MemberDraft{}.SetName("reject")); !errors.Is(err, timequeries.Veto) {
			return errors.New("timestamp convention bypassed a field mutator veto")
		}
		current, err = q.RequireFind(t.Context(), tx, member.ID)
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
		q, f := timequeries.QueryTimeMembers(), timequeries.MemberFields()
		members, err := q.CreateMany(ctx, tx, []timequeries.MemberDraft{timequeries.MemberDraft{}.SetName("first"), timequeries.MemberDraft{}.SetName("second")})
		if err != nil {
			return err
		}
		if len(members) != 2 || clock.calls.Load() != 1 || len(state.Changes) != 0 {
			return errors.New("bulk timestamps ran per row or dispatched observers")
		}
		for _, member := range members {
			if !member.CreatedAt.Equal(clock.stored()) || !member.UpdatedAt.UTC().Equal(clock.stored()) {
				return errors.New("bulk records did not share a timestamp")
			}
		}
		member := members[0]
		clock.source.Advance(time.Hour)
		policy := query.OnConflict(f.ID).DoUpdate(f.Name.Incoming())
		result, err := q.Upsert(ctx, tx, timequeries.MemberDraft{}.SetID(member.ID).SetName("updated"), policy)
		if err != nil {
			return err
		}
		updated, ok := result.Get()
		if !ok || !updated.CreatedAt.Equal(member.CreatedAt) || !updated.UpdatedAt.UTC().Equal(clock.stored().Add(2*time.Second)) || clock.calls.Load() != 2 {
			return errors.New("upsert did not retain creation time or normalized proposed update time")
		}
		clock.source.Advance(time.Hour)
		skipped, err := q.Upsert(ctx, tx, timequeries.MemberDraft{}.SetID(member.ID).SetName("skip"), query.OnConflict(f.ID).DoNothing())
		if err != nil || skipped.IsSet() {
			return errors.New("timestamp DO NOTHING changed conflict semantics")
		}
		current, err := q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current.UpdatedAt != updated.UpdatedAt {
			return errors.New("skipped conflict changed stored timestamp")
		}
		before := clock.calls.Load()
		if _, err := q.CreateMany(ctx, tx, nil); err != nil {
			return err
		}
		if _, err := q.UpsertMany(ctx, tx, nil, policy); err != nil {
			return err
		}
		if clock.calls.Load() != before {
			return errors.New("empty batch sampled application time")
		}
		explicit := clock.stored().Add(-24 * time.Hour)
		explicitUpdated, _ := temporal.NewDateTime(explicit.Add(time.Hour))
		manual, err := timequeries.QueryTimeManual().Create(t.Context(), tx, timequeries.ManualDraft{}.SetCreatedAt(explicit).SetUpdatedAt(explicitUpdated))
		if err != nil {
			return err
		}
		if !manual.CreatedAt.Equal(explicit) || manual.UpdatedAt != explicitUpdated || clock.calls.Load() != before {
			return errors.New("timestamps=false changed explicitly supplied fields")
		}
		if _, err := timequeries.QueryTimeManual().Update(t.Context(), tx, manual.ID, timequeries.ManualDraft{}); !errors.Is(err, fault.Invalid) {
			return errors.New("unmanaged empty update changed its contract")
		}
		if len(state.Changes) != 0 || len(state.Committed) != 0 {
			return errors.New("bulk timestamp path invoked normal observers")
		}
		return nil
	})
}
