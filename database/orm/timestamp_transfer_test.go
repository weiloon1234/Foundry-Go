package orm

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"reflect"
	"strings"
	"testing"
	"time"
)

type transferClock struct {
	now   time.Time
	calls int
}

func (c *transferClock) Now() time.Time { c.calls++; return c.now }
func transferTimedMutator(d timedTable, calls *int) {
	// Exercise the shared convention/mutator boundary. Native role declarations do
	// not expose a transform option for framework-generated timestamps.
	c := pivotColumn(d.core, "updated_at")
	c.mutator = querycore.NewFieldMutator("updated_at", codec.DateTime(), func(v temporal.DateTime) (temporal.DateTime, error) { *calls++; return v.Add(time.Hour) })
	d.core.mutators = []querycore.FieldMutator{c.mutator}
}
func transferTimedPrepare(t *testing.T, p writePlan, c *transferClock) (writePlan, querycore.Statement) {
	t.Helper()
	p, e := p.conventions(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	p, e = p.mutate(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	s, e := p.compile()
	if e != nil {
		t.Fatal(e)
	}
	return p, s
}
func TestModelTimestampsUseOneSampleAndPreserveInputs(t *testing.T) {
	d := timedRecords()
	calls := 0
	transferTimedMutator(d, &calls)
	c := &transferClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.FixedZone("test", 10800))}
	raw := []Assignment[timed]{d.Key().Set(1)}
	for range 2 {
		p, e := planWrite(d.Query().state, querycore.InsertModel, raw, false)
		if e != nil {
			t.Fatal(e)
		}
		prepared, s := transferTimedPrepare(t, p, c)
		now := c.now.UTC().Truncate(time.Microsecond)
		if !reflect.DeepEqual(s.Bindings, []any{int64(1), now, now.Add(time.Hour)}) || len(raw) != 1 || len(p.assignments) != 1 || len(prepared.assignments) != 3 {
			t.Fatal(s.Bindings)
		}
		c.now = c.now.Add(time.Hour)
	}
	if c.calls != 2 || calls != 2 {
		t.Fatal(c.calls, calls)
	}
}
func TestModelTimestampsPreserveExplicitCreationAndSkipPhysicalDelete(t *testing.T) {
	d := timedRecords()
	c := &transferClock{now: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)}
	old, e := temporal.NewDateTime(c.now.Add(-time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[timed]{d.Key().Set(1), d.CreatedAt.Set(old)}, false)
	if e != nil {
		t.Fatal(e)
	}
	_, s := transferTimedPrepare(t, p, c)
	if !reflect.DeepEqual(s.Bindings, []any{int64(1), old.UTC(), c.now}) {
		t.Fatal(s.Bindings)
	}
	p, e = planWrite(d.Query().Where(d.Key().Eq(3)).state, querycore.UpdateModel, []Assignment[timed](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	p, s = transferTimedPrepare(t, p, c)
	if len(p.assignments) != 1 || p.assignments[0].Field.Column != "updated_at" || !strings.Contains(s.SQLText, `SET "updated_at" = $1 WHERE`) {
		t.Fatal(s.SQLText)
	}
	p, e = planWrite(d.Query().WithTrashed().Where(d.Key().Eq(3)).state, querycore.ForceDeleteModel, []Assignment[timed](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	_, s = transferTimedPrepare(t, p, c)
	if !strings.HasPrefix(s.SQLText, "DELETE FROM") || c.calls != 2 {
		t.Fatal(s.SQLText, c.calls)
	}
}
func TestModelTimestampsBulkAndConflictUseNormalizedProposedValue(t *testing.T) {
	d := timedRecords()
	calls := 0
	transferTimedMutator(d, &calls)
	c := &transferClock{now: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)}
	p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[timed]{d.Key().Set(1)}, false)
	if e != nil {
		t.Fatal(e)
	}
	p.batch = [][]querycore.MutationAssignment{p.assignments, p.assignments}
	p.assignments = nil
	policy := OnConflict(ConflictKey(d.Key())).DoUpdate(ConflictIncoming(d.UpdatedAt))
	p, e = p.withConflict(&policy.state)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		_, s := transferTimedPrepare(t, p, c)
		if !strings.Contains(s.SQLText, `"updated_at" = "excluded"."updated_at"`) || !reflect.DeepEqual(s.Bindings, []any{int64(1), c.now, c.now.Add(time.Hour), int64(1), c.now, c.now.Add(time.Hour)}) {
			t.Fatal(s.SQLText, s.Bindings)
		}
		if len(p.batch[0]) != 1 || len(policy.state.core.Updates) != 1 {
			t.Fatal("source mutated")
		}
	}
	if c.calls != 2 || calls != 4 {
		t.Fatal(c.calls, calls)
	}
	for _, bad := range []Conflict[timed]{OnConflict(ConflictKey(d.Key())).DoUpdate(), OnConflict(ConflictKey(d.Key())).DoUpdate(ConflictIncoming(d.UpdatedAt), ConflictIncoming(d.UpdatedAt))} {
		if _, e := p.withConflict(&bad.state); !errors.Is(e, fault.Invalid) {
			t.Fatal("timestamp repaired invalid policy", e)
		}
	}
	if c.calls != 2 {
		t.Fatal("invalid policy sampled")
	}
	writer := &noEachWriter{}
	if out, e := d.InsertMany(t.Context(), writer, nil); e != nil || len(out) != 0 || writer.calls != 0 || c.calls != 2 {
		t.Fatal(out, e, writer.calls)
	}
}
func TestModelTimestampsRejectInvalidTimeAndCancellation(t *testing.T) {
	d := timedRecords()
	p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[timed]{d.Key().Set(1)}, false)
	if e != nil {
		t.Fatal(e)
	}
	c := &transferClock{now: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
	if _, e := p.conventions(t.Context(), c); !errors.Is(e, fault.Invalid) {
		t.Fatal("invalid year", e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := p.conventions(ctx, c); !errors.Is(e, context.Canceled) || c.calls != 1 {
		t.Fatal("cancel sampled", e, c.calls)
	}
}
func TestSoftDeleteConventionsShareTimeAndRunMutatorsOnce(t *testing.T) {
	d := timedRecords()
	calls := 0
	cfield := pivotColumn(d.core, "deleted_at")
	cfield.mutator = querycore.NewNullableFieldMutator("deleted_at", codec.DateTime(), func(v temporal.DateTime) (temporal.DateTime, error) { calls++; return v.Add(time.Minute) })
	d.core.mutators = []querycore.FieldMutator{cfield.mutator}
	c := &transferClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	p, e := planWrite(d.Query().Where(d.Key().Eq(4)).state, querycore.DeleteModel, []Assignment[timed](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	p, s := transferTimedPrepare(t, p, c)
	now := c.now.Truncate(time.Microsecond)
	if !strings.HasPrefix(s.SQLText, `UPDATE "timed" SET`) || !reflect.DeepEqual(s.Bindings, []any{now, now.Add(time.Minute), int64(4)}) || c.calls != 1 || calls != 1 || p.kind.Operation() != lifecycle.SoftDelete || len(p.assignments) != 2 {
		t.Fatal(s.SQLText, s.Bindings, c.calls, calls)
	}
	c.now = c.now.Add(time.Hour)
	p, e = planWrite(d.Query().OnlyTrashed().Where(d.Key().Eq(4)).state, querycore.RestoreModel, []Assignment[timed](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	p, s = transferTimedPrepare(t, p, c)
	if !reflect.DeepEqual(s.Bindings, []any{c.now.Truncate(time.Microsecond), nil, int64(4)}) || c.calls != 2 || calls != 1 || !strings.Contains(s.SQLText, `"deleted_at" IS NOT NULL`) || p.kind.Operation() != lifecycle.Restore {
		t.Fatal(s.SQLText, s.Bindings, c.calls, calls)
	}
	p, e = planWrite(d.Query().WithTrashed().Where(d.Key().Eq(4)).state, querycore.ForceDeleteModel, []Assignment[timed](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	p, s = transferTimedPrepare(t, p, c)
	if !strings.HasPrefix(s.SQLText, "DELETE") || c.calls != 2 || calls != 1 || len(p.assignments) != 0 || p.kind.Operation() != lifecycle.ForceDelete {
		t.Fatal(s.SQLText, c.calls, calls)
	}
}
func TestSoftDeleteRestoreWithoutTimestampsDoesNotReadClock(t *testing.T) {
	d := people()
	p, e := planWrite(d.Query().OnlyTrashed().Where(d.ID.Eq(8)).state, querycore.RestoreModel, []Assignment[relationPerson](nil), false)
	if e != nil {
		t.Fatal(e)
	}
	p, e = p.conventions(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	s, e := p.compile()
	if e != nil || !reflect.DeepEqual(s.Bindings, []any{nil, int64(8)}) {
		t.Fatal(s.Bindings, e)
	}
	plain := scopeTransferFields()
	for _, kind := range []querycore.MutationKind{querycore.SoftDeleteModel, querycore.RestoreModel, querycore.ForceDeleteModel, 255} {
		if _, e := planWrite(plain.Query().Where(plain.Key().Eq(8)).state, kind, []Assignment[scopeTransferRow](nil), false); !errors.Is(e, fault.Invalid) {
			t.Fatal(kind, e)
		}
	}
}
