package softqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresSoftDeleteRestoreForceDeleteLifecycle(t *testing.T) {
	trace := &softqueries.Trace{}
	runSoft(t, func(tx *database.Tx, clock *testkit.Clock) error {
		q := softqueries.Members.Query()
		member, err := softqueries.Members.Insert(t.Context(), tx, softqueries.Members.Name.Set(" member "))
		if err != nil {
			return err
		}
		kept, err := softqueries.Members.Insert(t.Context(), tx, softqueries.Members.Name.Set("kept"))
		if err != nil {
			return err
		}
		ctx := softqueries.WithTrace(t.Context(), trace)
		clock.Advance(time.Hour)
		deleted, err := orm.Delete(ctx, tx, q, softqueries.Members.Key(), member.ID)
		if err != nil {
			return err
		}
		deletionTime, present := deleted.DeletedAt.Get()
		if !present || !deletionTime.Equal(clock.Now().Truncate(time.Microsecond)) || !deleted.UpdatedAt.UTC().Equal(deletionTime) || !deleted.CreatedAt.Equal(member.CreatedAt) {
			return errors.New("soft deletion lost its shared time or complete stored result")
		}
		if !reflect.DeepEqual(trace.Steps, []string{"local.deleting", "provider.deleting", "local.deleted", "provider.deleted"}) {
			return errors.New("soft deletion ran the wrong callback stages")
		}
		if len(trace.ORMChanges) != 2 || !trace.ORMChanges[0].Before().IsSet() || !trace.ORMChanges[0].After().IsSet() || !checkedChange(orm.NullableChangeOf(trace.ORMChanges[0], softqueries.Members.DeletedAt)).Assigned() || !checkedChange(orm.ChangeOf(trace.ORMChanges[0], softqueries.Members.UpdatedAt)).Changed() {
			return errors.New("soft deletion did not capture stored field changes")
		}
		if current, err := q.Where(softqueries.Members.Key().Eq(member.ID)).First(ctx, tx); err != nil || current.IsSet() {
			return errors.New("ordinary lookup exposed a deleted model")
		}
		locked, err := q.ForUpdate().OnlyTrashed().Where(softqueries.Members.Key().Eq(member.ID)).Require(ctx, tx)
		if err != nil {
			return err
		}
		if locked.DeletedAt.IsNull() {
			return errors.New("locked query lost deleted-model visibility")
		}
		if found, err := q.WithTrashed().Where(softqueries.Members.Key().Eq(member.ID)).Require(ctx, tx); err != nil || found.Name != member.Name {
			return errors.New("WithTrashed lost the stored model")
		}
		if isDeleted, err := deleted.AccessDeletedAt(); err != nil || !isDeleted {
			return errors.New("explicit deletion getter did not retain the stored instant")
		}
		beforeSteps := len(trace.Steps)
		for _, candidate := range []softqueries.Member{member, kept} {
			if _, err := orm.Delete(ctx, tx, q.OnlyTrashed(), softqueries.Members.Key(), candidate.ID); !errors.Is(err, database.NotFound) {
				return errors.New("OnlyTrashed Delete changed a model's state")
			}
		}
		if len(trace.Steps) != beforeSteps {
			return errors.New("unmatched deletion invoked hooks")
		}
		clock.Advance(time.Hour)
		trace.Steps = nil
		restored, err := softqueries.Members.Restore(ctx, tx, member.ID)
		if err != nil {
			return err
		}
		if !restored.DeletedAt.IsNull() || !restored.UpdatedAt.UTC().Equal(clock.Now().Truncate(time.Microsecond)) {
			return errors.New("restoration did not clear deletion and update time")
		}
		if !reflect.DeepEqual(trace.Steps, []string{"local.restoring", "provider.restoring", "local.restored", "provider.restored"}) {
			return errors.New("restore emitted ordinary saving/updating stages")
		}
		compared, err := orm.Compare(softqueries.Members.Table, value.Set(restored), value.Set(restored))
		if err != nil {
			return err
		}
		if compared.Operation().IsSet() {
			return errors.New("standalone comparison invented a lifecycle event")
		}
		clock.Advance(time.Hour)
		trace.Steps = nil
		removed, err := orm.ForceDelete(ctx, tx, q.WithTrashed(), softqueries.Members.Soft, softqueries.Members.Key(), member.ID)
		if err != nil {
			return err
		}
		if removed.UpdatedAt != restored.UpdatedAt || trace.ORMChanges[len(trace.ORMChanges)-1].After().IsSet() {
			return errors.New("force delete updated time or retained a stored after-model")
		}
		if !reflect.DeepEqual(trace.Steps, []string{"local.force-deleting", "provider.force-deleting", "local.deleting", "provider.deleting", "local.deleted", "provider.deleted", "local.force-deleted", "provider.force-deleted"}) {
			return errors.New("force deletion callback order changed")
		}
		if current, err := q.WithTrashed().Where(softqueries.Members.Key().Eq(member.ID)).First(ctx, tx); err != nil || current.IsSet() {
			return errors.New("force deletion left a stored model")
		}
		if _, err := q.Where(softqueries.Members.Key().Eq(kept.ID)).Require(ctx, tx); err != nil {
			return err
		}
		if len(trace.Committed) != 0 {
			return errors.New("nested deletion committed before its parent")
		}
		return nil
	})
	if !reflect.DeepEqual(trace.Committed, []lifecycle.Operation{lifecycle.SoftDelete, lifecycle.SoftDelete, lifecycle.Restore, lifecycle.Restore, lifecycle.ForceDelete, lifecycle.ForceDelete}) {
		t.Fatal("special lifecycle writes lost outer commit order", trace.Committed)
	}
}

func TestPostgresSoftDeleteVetoAndRestoreConflictRollback(t *testing.T) {
	runSoft(t, func(tx *database.Tx, clock *testkit.Clock) error {
		q := softqueries.Members.Query()
		member, err := softqueries.Members.Insert(t.Context(), tx, softqueries.Members.Name.Set("same"))
		if err != nil {
			return err
		}
		veto := &softqueries.Trace{VetoAt: "local.deleted"}
		if _, err := orm.Delete(softqueries.WithTrace(t.Context(), veto), tx, q, softqueries.Members.Key(), member.ID); !errors.Is(err, softqueries.Veto) {
			return errors.New("post-delete callback did not veto persistence")
		}
		current, err := q.Where(softqueries.Members.Key().Eq(member.ID)).Require(t.Context(), tx)
		if err != nil {
			return err
		}
		if !current.DeletedAt.IsNull() || current.UpdatedAt != member.UpdatedAt || len(veto.Committed) != 0 {
			return errors.New("veto failed to roll back soft deletion")
		}
		clock.Advance(time.Hour)
		deleted, err := orm.Delete(t.Context(), tx, q, softqueries.Members.Key(), member.ID)
		if err != nil {
			return err
		}
		other, err := softqueries.Members.Insert(t.Context(), tx, softqueries.Members.Name.Set("same"))
		if err != nil {
			return err
		}
		if _, err := softqueries.Members.Restore(t.Context(), tx, member.ID); !errors.Is(err, database.UniqueViolation) {
			return errors.New("restore ignored a conflicting active unique value")
		}
		current, err = q.WithTrashed().Where(softqueries.Members.Key().Eq(member.ID)).Require(t.Context(), tx)
		if err != nil {
			return err
		}
		if current.DeletedAt != deleted.DeletedAt || current.UpdatedAt != deleted.UpdatedAt {
			return errors.New("failed restore changed its model")
		}
		if _, err := q.Where(softqueries.Members.Key().Eq(other.ID)).Require(t.Context(), tx); err != nil {
			return err
		}
		rollback := errors.New("outer rollback")
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if _, err := orm.ForceDelete(t.Context(), child, q.WithTrashed(), softqueries.Members.Soft, softqueries.Members.Key(), member.ID); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			if err == nil {
				return errors.New("outer savepoint unexpectedly committed")
			}
			return err
		}
		if _, err := q.WithTrashed().Where(softqueries.Members.Key().Eq(member.ID)).Require(t.Context(), tx); err != nil {
			return err
		}
		// The unannotated natural-key model exercises the no-local-observer path and
		// a wrapper whose supplied transaction owns its clock and observer metadata.
		groups := softqueries.Groups.Query()
		group, err := softqueries.Groups.Insert(t.Context(), tx, softqueries.Groups.Key().Set("team"), softqueries.Groups.Name.Set("team"))
		if err != nil {
			return err
		}
		group, err = orm.Delete(t.Context(), wrappedTransactor{tx}, groups, softqueries.Groups.Key(), group.Code)
		if err != nil {
			return err
		}
		if group.DeletedAt.IsNull() {
			return errors.New("wrapper skipped soft-delete conventions")
		}
		group, err = softqueries.Groups.Restore(t.Context(), wrappedTransactor{tx}, group.Code)
		if err != nil {
			return err
		}
		if !group.DeletedAt.IsNull() {
			return errors.New("restore without managed UpdatedAt did not clear deletion")
		}
		return nil
	})
}

func TestPostgresSoftDeleteCancellationStopsLaterStages(t *testing.T) {
	runSoft(t, func(tx *database.Tx, clock *testkit.Clock) error {
		q := softqueries.Members.Query()
		member, err := softqueries.Members.Insert(t.Context(), tx, softqueries.Members.Name.Set("cancel"))
		if err != nil {
			return err
		}
		clock.Advance(time.Hour)
		for _, point := range []string{"local.deleting", "local.deleted"} {
			ctx, cancel := context.WithCancel(t.Context())
			trace := &softqueries.Trace{CancelAt: point, Cancel: cancel}
			result, err := orm.Delete(softqueries.WithTrace(ctx, trace), tx, q, softqueries.Members.Key(), member.ID)
			cancel()
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, softqueries.Member{}) {
				return errors.New("soft-delete cancellation did not discard its result")
			}
			expected := []string{"local.deleting"}
			if point == "local.deleted" {
				expected = []string{"local.deleting", "provider.deleting", "local.deleted"}
			}
			if !reflect.DeepEqual(trace.Steps, expected) || len(trace.Committed) != 0 {
				return errors.New("cancellation ran later callback stages or committed")
			}
			current, err := q.Where(softqueries.Members.Key().Eq(member.ID)).Require(t.Context(), tx)
			if err != nil {
				return err
			}
			if !current.DeletedAt.IsNull() || current.UpdatedAt != member.UpdatedAt {
				return errors.New("canceled deletion changed stored state")
			}
		}
		_, err = orm.Delete(t.Context(), tx, q, softqueries.Members.Key(), member.ID)
		return err
	})
}

func checkedChange[V any](field lifecycle.FieldChange[V], err error) lifecycle.FieldChange[V] {
	if err != nil {
		panic(err)
	}
	return field
}
