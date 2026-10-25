package linkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresRelationAttachDetachLifecycle(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		r := linkqueries.MemberLinks.Groups
		ctx := linkqueries.WithTrace(t.Context(), trace)
		draft := []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.Role.Set(" ADMIN ")}
		first, err := orm.Attach(ctx, tx, r, member, group, draft...)
		if err != nil {
			return err
		}
		if first.MemberID != member.ID || first.GroupCode != group.Code || first.Role != "admin" || !first.CreatedAt.Equal(clock.Now().Truncate(time.Microsecond)) {
			return errors.New("attachment lost keys, transformation or managed timestamps")
		}
		if trace.Reads != 0 {
			return errors.New("write ownership reads dispatched Retrieved")
		}
		expected := []string{"local.saving", "provider.saving", "local.creating", "provider.creating", "local.created", "provider.created", "local.saved", "provider.saved"}
		if !reflect.DeepEqual(trace.Steps, expected) {
			return errors.New("attachment bypassed ordinary pivot lifecycle ordering")
		}
		// Reuse the caller's slice without Attach's derived keys. Abort before SQL
		// so this inspection cannot add callbacks or change persisted state.
		inspected := false
		inspectDone := errors.New("caller draft inspected")
		trace.InspectMembership = func(w *orm.Write[linkqueries.Membership]) error {
			fields := linkqueries.Memberships
			role, ok := orm.WriteGet(w, fields.Role.Set).Get()
			if !ok || role != " ADMIN " || orm.WriteIsSet(w, fields.MemberID.Set) || orm.WriteIsSet(w, fields.GroupCode.Set) || orm.WriteIsSet(w, fields.Key().Set) || orm.WriteIsSet(w, fields.UpdatedAt.Set) {
				return errors.New("attachment changed caller values or omissions")
			}
			inspected = true
			return inspectDone
		}
		_, inspectErr := linkqueries.Memberships.Insert(ctx, tx, draft...)
		trace.InspectMembership = nil
		if !inspected || !errors.Is(inspectErr, inspectDone) {
			return errors.New("caller draft inspection failed")
		}
		changes := trace.ORMChanges[0]
		memberChange, err := orm.ChangeOf(changes, linkqueries.Memberships.MemberID)
		if err != nil {
			return err
		}
		groupChange, err := orm.ChangeOf(changes, linkqueries.Memberships.GroupCode)
		if err != nil {
			return err
		}
		roleChange, err := orm.ChangeOf(changes, linkqueries.Memberships.Role)
		if err != nil {
			return err
		}
		if !memberChange.Assigned() || !groupChange.Assigned() || !roleChange.Assigned() {
			return errors.New("derived keys missing from pivot changes")
		}
		if operation, ok := changes.Operation().Get(); !ok || operation != lifecycle.Create {
			return errors.New("attachment operation is not Create")
		}
		second, err := orm.Attach(ctx, tx, r, member, group, draft...)
		if err != nil {
			return err
		}
		if second.ID == first.ID {
			return errors.New("duplicate link silently reused its pivot")
		}
		trace.Steps = nil
		if _, err := orm.Detach(ctx, tx, orm.WithRelationWriteLimit(r, 1), member, group); !errors.Is(err, fault.Invalid) {
			return errors.New("oversized detach silently truncated links")
		}
		if len(trace.Steps) != 0 {
			return errors.New("oversized detach invoked pivot hooks")
		}
		clock.Advance(time.Hour)
		detached, err := orm.Detach(ctx, tx, r, member, group)
		if err != nil {
			return err
		}
		if len(detached) != 2 {
			return errors.New("detach lost duplicate links")
		}
		for _, pivot := range detached {
			instant, ok := pivot.DeletedAt.Get()
			if !ok || !instant.Equal(clock.Now().Truncate(time.Microsecond)) {
				return errors.New("detach bypassed soft deletion")
			}
		}
		expected = []string{"local.deleting", "provider.deleting", "local.deleted", "provider.deleted", "local.deleting", "provider.deleting", "local.deleted", "provider.deleted"}
		if !reflect.DeepEqual(trace.Steps, expected) {
			return errors.New("detach did not run one normal lifecycle per pivot")
		}
		trace.Steps = nil
		none, err := orm.Detach(ctx, tx, r.OnlyTrashedPivot(), member, group)
		if err != nil {
			return err
		}
		if len(none) != 0 || len(trace.Steps) != 0 {
			return errors.New("ordinary detach changed already-deleted links")
		}
		removed, err := orm.ForceDetach(ctx, tx, r.WithTrashedPivot(), linkqueries.Memberships.Soft, member, group)
		if err != nil {
			return err
		}
		if len(removed) != 2 {
			return errors.New("force detach lost deleted links")
		}
		for _, changes := range trace.ORMChanges[len(trace.ORMChanges)-8:] {
			if changes.After().IsSet() {
				return errors.New("force-detach changes retained an after-model")
			}
		}
		count, err := linkqueries.Memberships.Query().WithTrashed().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("force detach retained pivots")
		}
		if len(trace.Committed) != 0 {
			return errors.New("relation callback ran before outer commit")
		}
		return nil
	})
	want := []lifecycle.Operation{lifecycle.Create, lifecycle.Create, lifecycle.Create, lifecycle.Create, lifecycle.SoftDelete, lifecycle.SoftDelete, lifecycle.SoftDelete, lifecycle.SoftDelete, lifecycle.ForceDelete, lifecycle.ForceDelete, lifecycle.ForceDelete, lifecycle.ForceDelete}
	if !reflect.DeepEqual(trace.Committed, want) {
		t.Fatal("relation writes lost outer-commit callback operations", trace.Committed)
	}
}

func TestPostgresRelationScopesAndAtomicRollback(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		other, err := linkqueries.Members.Insert(t.Context(), tx, linkqueries.Members.Name.Set("other"))
		if err != nil {
			return err
		}
		r := linkqueries.MemberLinks.Groups
		ctx := linkqueries.WithTrace(t.Context(), trace)
		if _, err := orm.Attach(ctx, tx, r, member, group, linkqueries.Memberships.MemberID.Set(other.ID), linkqueries.Memberships.Role.Set("admin")); !errors.Is(err, fault.Invalid) {
			return errors.New("attachment escaped its source endpoint")
		}
		trace.Steps = nil
		target := linkqueries.Groups
		if _, err := orm.Attach(ctx, tx, r.Where(target.Name.Eq("denied")), member, group, linkqueries.Memberships.Role.Set("admin")); !errors.Is(err, database.NotFound) {
			return errors.New("attachment ignored target scope")
		}
		if len(trace.Steps) != 0 {
			return errors.New("missing endpoint invoked pivot hooks")
		}
		pivot := linkqueries.Memberships
		if _, err := orm.Attach(ctx, tx, r.WherePivot(pivot.Role.Eq("admin")), member, group, linkqueries.Memberships.Role.Set("viewer")); !errors.Is(err, fault.Invalid) {
			return errors.New("attachment ignored stored pivot scope")
		}
		trace.VetoAt = "local.created"
		if _, err := orm.Attach(ctx, tx, r, member, group, linkqueries.Memberships.Role.Set("admin")); !errors.Is(err, linkqueries.ErrVeto) {
			return errors.New("post-insert veto did not abort attachment")
		}
		trace.VetoAt = ""
		if count, err := linkqueries.Memberships.Query().WithTrashed().Count(t.Context(), tx); err != nil || count != 0 {
			return errors.New("failed attachment retained a pivot")
		}
		for range 2 {
			if _, err := orm.Attach(t.Context(), tx, r, member, group, linkqueries.Memberships.Role.Set("admin")); err != nil {
				return err
			}
		}
		trace.Deletions = 0
		trace.VetoDeletion = 2
		if _, err := orm.Detach(ctx, tx, r, member, group); !errors.Is(err, linkqueries.ErrVeto) {
			return errors.New("later pivot veto did not abort detach")
		}
		if count, err := linkqueries.Memberships.Query().Count(t.Context(), tx); err != nil || count != 2 {
			return errors.New("later veto retained an earlier deletion")
		}
		return nil
	})
	if len(trace.Committed) != 0 {
		t.Fatal("rolled-back relation writes retained after-commit callbacks")
	}
}

func TestPostgresRelationCancellationAndParentReuse(t *testing.T) {
	for _, phase := range []string{"local.creating", "local.created", "local.deleted"} {
		t.Run(phase, func(t *testing.T) {
			trace := &linkqueries.Trace{CancelAt: phase}
			runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				member, group, err := ormEndpoints(t, tx)
				if err != nil {
					return err
				}
				r := linkqueries.MemberLinks.Groups
				if phase == "local.deleted" {
					if _, err := orm.Attach(t.Context(), tx, r, member, group, linkqueries.Memberships.Role.Set("admin")); err != nil {
						return err
					}
				}
				canceled, cancel := context.WithCancel(t.Context())
				defer cancel()
				trace.Cancel = cancel
				ctx := linkqueries.WithTrace(canceled, trace)
				if phase == "local.deleted" {
					_, err = orm.Detach(ctx, tx, r, member, group)
				} else {
					_, err = orm.Attach(ctx, tx, r, member, group, linkqueries.Memberships.Role.Set("admin"))
				}
				if !errors.Is(err, context.Canceled) {
					return errors.New("relation write lost cancellation")
				}
				count, err := linkqueries.Memberships.Query().Count(t.Context(), tx)
				if err != nil {
					return err
				}
				wanted := int64(0)
				if phase == "local.deleted" {
					wanted = 1
				}
				if count != wanted {
					return errors.New("canceled relation write persisted changes")
				}
				return nil
			})
			if len(trace.Committed) != 0 {
				t.Fatal("canceled relation write retained after-commit callbacks")
			}
		})
	}
}
