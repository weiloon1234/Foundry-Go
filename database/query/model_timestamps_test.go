package query

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type timestampRecord struct {
	id      int
	name    string
	created time.Time
	updated temporal.DateTime
}
type timestampClock struct {
	now   time.Time
	calls int
}

func (c *timestampClock) Now() time.Time { c.calls++; return c.now }

func timestampQuery(mutate func(temporal.DateTime) (temporal.DateTime, error)) Query[timestampRecord] {
	return ForModel(Define("timed_records", "id", []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "created_on"}, {Name: "modified_on"}}, func(database.Row) (timestampRecord, error) { return timestampRecord{}, nil },
		NewModelField("created_on", codec.Time(), func(r timestampRecord) time.Time { return r.created }),
		NewMutatedModelField("modified_on", codec.DateTime(), func(r timestampRecord) temporal.DateTime { return r.updated }, mutate),
	).WithTimestamps("created_on", "modified_on"))
}

func TestModelTimestampsUseOneSampleAndPreserveInputs(t *testing.T) {
	calls := 0
	q := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { calls++; return v.Add(time.Hour) })
	source := &timestampClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.FixedZone("test", 3*3600))}
	raw := Change(Assign[timestampRecord]("timed_records", "name", codec.String[string](), "first"))
	for range 2 {
		plan := mutationPlan[timestampRecord]{query: q, kind: insertModel, mutation: raw}
		statement, err := prepareMutation(t.Context(), &plan, source)
		if err != nil {
			t.Fatal(err)
		}
		now := source.now.UTC().Truncate(time.Microsecond)
		if !reflect.DeepEqual(statement.Arguments(), []any{"first", now, now.Add(time.Hour)}) {
			t.Fatal("managed timestamps lost clock precision, codecs or mutator order", statement.Arguments())
		}
		if len(raw.assignments) != 1 || len(plan.mutation.assignments) != 3 {
			t.Fatal("automatic assignments altered caller input or were not captured")
		}
		source.now = source.now.Add(time.Hour)
	}
	if source.calls != 2 || calls != 2 {
		t.Fatal("timestamp clock or mutator ran more than once per attempt", source.calls, calls)
	}
}

func TestModelTimestampsPreserveExplicitCreationAndSkipPhysicalDelete(t *testing.T) {
	q := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { return v, nil })
	source := &timestampClock{now: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)}
	old := source.now.Add(-time.Hour)
	raw := Change(Assign[timestampRecord]("timed_records", "name", codec.String[string](), "first"), Assign[timestampRecord]("timed_records", "created_on", codec.Time(), old))
	plan := mutationPlan[timestampRecord]{query: q, kind: insertModel, mutation: raw}
	statement, err := prepareMutation(t.Context(), &plan, source)
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{"first", old, source.now}) {
		t.Fatal("explicit creation timestamp replaced", err)
	}
	id := NewScalarField[timestampRecord, int]("timed_records", "id", codec.Signed[int]())
	plan = mutationPlan[timestampRecord]{query: q.Where(id.Eq(3)), kind: updateModel}
	statement, err = prepareMutation(t.Context(), &plan, source)
	if err != nil || !strings.Contains(statement.SQL(), `SET "modified_on" = $1 WHERE`) {
		t.Fatal("timestamp-only update failed", err)
	}
	if len(plan.mutation.assignments) != 1 || plan.mutation.assignments[0].field.column != "modified_on" {
		t.Fatal("update assigned the creation time")
	}
	plan = mutationPlan[timestampRecord]{query: q.Where(id.Eq(3)), kind: deleteModel}
	statement, err = prepareMutation(t.Context(), &plan, source)
	if err != nil || !strings.HasPrefix(statement.SQL(), "DELETE FROM") || source.calls != 2 {
		t.Fatal("physical delete sampled application time", err, source.calls)
	}
}

func TestModelTimestampsBulkAndConflictUseNormalizedProposedValue(t *testing.T) {
	calls := 0
	q := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { calls++; return v.Add(time.Hour) })
	source := &timestampClock{now: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)}
	id := NewScalarField[timestampRecord, int]("timed_records", "id", codec.Signed[int]())
	name := NewTextField[timestampRecord, string]("timed_records", "name", codec.String[string]())
	policy := OnConflict(id).DoUpdate(name.Incoming())
	row := Change(Assign[timestampRecord]("timed_records", "name", codec.String[string](), "first"))
	plan := insertPlan[timestampRecord]{query: q, rows: []Mutation[timestampRecord]{row, row}, conflict: &policy}
	for range 2 {
		prepared, err := plan.withTimestamps(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		statement, err := prepared.prepare(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(statement.SQL(), `"modified_on" = "excluded"."modified_on"`) {
			t.Fatal("timestamp conflict failed to copy the normalized proposed value", statement.SQL())
		}
		if len(plan.rows[0].assignments) != 1 || len(policy.updates) != 1 {
			t.Fatal("timestamp batch changed original inputs")
		}
		want := []any{"first", source.now, source.now.Add(time.Hour), "first", source.now, source.now.Add(time.Hour)}
		if !reflect.DeepEqual(statement.Arguments(), want) {
			t.Fatal("batch clock sample or mutator output changed", statement.Arguments())
		}
	}
	if source.calls != 2 || calls != 4 {
		t.Fatal("bulk/upsert transformed or sampled time twice", source.calls, calls)
	}
	for _, policy := range []Conflict[timestampRecord]{OnConflict(id).DoUpdate(), OnConflict(id).DoUpdate(name.Set("a"), name.Set("b"))} {
		bad := insertPlan[timestampRecord]{query: q, rows: []Mutation[timestampRecord]{row}, conflict: &policy}
		if _, err := bad.withTimestamps(t.Context(), source); !errors.Is(err, fault.Invalid) {
			t.Fatal("timestamps repaired an invalid original policy", err)
		}
	}
	if source.calls != 2 {
		t.Fatal("invalid policy sampled the clock")
	}
	empty := insertPlan[timestampRecord]{query: q}
	prepared, err := empty.withTimestamps(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if statement, err := prepared.prepare(t.Context()); err != nil || statement.SQL() != "" || source.calls != 2 {
		t.Fatal("empty timestamp batch changed execution", err)
	}
}

func TestModelTimestampsRejectInvalidTimeCancellationAndMetadata(t *testing.T) {
	q := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { return v, nil })
	source := &timestampClock{now: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
	if _, err := modelTimestamp(t.Context(), source); !errors.Is(err, fault.Invalid) {
		t.Fatal("out-of-range clock instant accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := modelTimestamp(ctx, source); !errors.Is(err, context.Canceled) || source.calls != 1 {
		t.Fatal("canceled operation sampled clock", err)
	}
	for _, names := range [][2]string{{"created_on", "created_on"}, {"name", "modified_on"}, {"missing", "modified_on"}, {"id", "modified_on"}} {
		if err := q.definition.WithTimestamps(names[0], names[1]).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid timestamp metadata accepted", names, err)
		}
	}
	if err := q.definition.Validate(); err != nil {
		t.Fatal("valid timestamp metadata rejected", err)
	}
}
