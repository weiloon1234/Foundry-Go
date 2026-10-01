package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSoftDeleteConventionsShareTimeAndRunMutatorsOnce(t *testing.T) {
	q := softQuery()
	d := *q.definition
	d.columns = append(append([]Column(nil), d.columns...), Column{Name: "created_on"}, Column{Name: "updated_on"})
	d.modelFields = append([]ModelField[softRecord](nil), d.modelFields...)
	calls := 0
	d.modelFields[2] = NewNullableMutatedModelField("deleted_on", codec.Time(), func(r softRecord) value.Nullable[time.Time] { return r.deleted }, func(v time.Time) (time.Time, error) { calls++; return v.Add(time.Minute), nil })
	d.modelFields = append(d.modelFields,
		NewModelField("created_on", codec.Time(), func(softRecord) time.Time { return time.Time{} }),
		NewModelField("updated_on", codec.Time(), func(softRecord) time.Time { return time.Time{} }))
	q = ForModel(d.WithTimestamps("created_on", "updated_on"))
	source := &timestampClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	plan := mutationPlan[softRecord]{query: q.Where(softID().Eq(4)), kind: softDeleteModel}
	statement, err := prepareMutation(t.Context(), &plan, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := source.now.Truncate(time.Microsecond)
	if !strings.HasPrefix(statement.SQL(), `UPDATE "soft_records" SET`) || !reflect.DeepEqual(statement.Arguments(), []any{now.Add(time.Minute), now, int64(4)}) || source.calls != 1 || calls != 1 {
		t.Fatal("soft deletion lost its single sample or field mutator", statement.SQL(), statement.Arguments(), source.calls, calls)
	}
	if plan.kind.operation() != lifecycle.SoftDelete || len(plan.mutation.assignments) != 2 {
		t.Fatal("soft deletion lost event identity or effective assignments")
	}
	source.now = source.now.Add(time.Hour)
	plan = mutationPlan[softRecord]{query: q.OnlyTrashed().Where(softID().Eq(4)), kind: restoreModel}
	statement, err = prepareMutation(t.Context(), &plan, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{nil, source.now.Truncate(time.Microsecond), int64(4)}) || source.calls != 2 || calls != 1 || !strings.Contains(statement.SQL(), `"deleted_on" IS NOT NULL`) {
		t.Fatal("restoration lost null or applied a scalar mutator", statement.Arguments(), source.calls, calls)
	}
	if plan.kind.operation() != lifecycle.Restore {
		t.Fatal("restore reported an ordinary update")
	}
	plan = mutationPlan[softRecord]{query: q.WithTrashed().Where(softID().Eq(4)), kind: forceDeleteModel}
	statement, err = prepareMutation(t.Context(), &plan, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(statement.SQL(), `DELETE FROM "soft_records"`) || source.calls != 2 || calls != 1 || len(plan.mutation.assignments) != 0 || plan.kind.operation() != lifecycle.ForceDelete {
		t.Fatal("force deletion applied update conventions", statement.SQL())
	}
}

func TestSoftDeleteRestoreWithoutTimestampsDoesNotReadClock(t *testing.T) {
	q := softQuery().OnlyTrashed().Where(softID().Eq(8))
	plan := mutationPlan[softRecord]{query: q, kind: restoreModel}
	statement, err := prepareMutation(t.Context(), &plan, nil, nil)
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{nil, int64(8)}) {
		t.Fatal("restoration without update time required a clock", err, statement.Arguments())
	}
	for _, kind := range []mutationKind{softDeleteModel, restoreModel, forceDeleteModel, 255} {
		plain := *q.definition
		plain.softDelete = nil
		plan = mutationPlan[softRecord]{query: ForModel(plain).Where(softID().Eq(8)), kind: kind}
		if _, err := prepareMutation(t.Context(), &plan, nil, nil); !errors.Is(err, fault.Invalid) {
			t.Fatal("special write accepted an unsupported model or kind", kind, err)
		}
	}
}
