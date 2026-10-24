package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func rejectSensitiveCursor[V any](t *testing.T, source ValueQuerySource[V]) {
	t.Helper()
	base := ValueCursorFor(source)
	cursor := base.UniqueBy(base.Key())
	if _, err := cursor.Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "sensitive fields") {
		t.Fatal("sensitive cursor compiled or failed for an unrelated reason", err)
	}
	ex := &pageFailureExecutor{err: errors.New("unexpected execution")}
	page, err := cursor.Paginate(t.Context(), ex, CursorRequest[V]{Size: 1})
	if !errors.Is(err, fault.Invalid) || ex.calls != 0 || page.Items != nil || page.Next.IsSet() {
		t.Fatal("sensitive cursor reached execution", err, ex.calls)
	}
}

func TestRelatedAggregateConditionalCursorSensitivity(t *testing.T) {
	parent := cursorQuery()
	public := codec.Signed[int64]()
	secret := public.WithSensitiveValues()
	id := NewOrderedField[cursorRecord, int64]("records", "id", public)
	for _, registered := range []bool{false, true} {
		fields := []ModelField[cursorRecord]{NewModelField("id", public, func(r cursorRecord) int64 { return r.ID })}
		if registered {
			fields = append(fields, NewModelField("rank", codec.Nullable(secret), func(r cursorRecord) value.Nullable[int64] { return r.Rank }))
		}
		target := ForModel(Define("records", "id", []Column{{Name: "id"}, {Name: "rank", Nullable: true}}, func(database.Row) (cursorRecord, error) { return cursorRecord{}, nil }, fields...))
		// When model metadata supplies the mark, deliberately use an ordinary
		// expression codec to prove the relation source retains its own provenance.
		fieldCodec := secret
		if registered {
			fieldCodec = public
		}
		field := NewNullableOrderedField[cursorRecord, int64]("records", "rank", fieldCodec)
		through := ManyToMany(id, id, id, id).Bind("Links", parent, target, target, func(cursorRecord) relation.Through[cursorRecord, cursorRecord] {
			return relation.Through[cursorRecord, cursorRecord]{}
		}, func(r cursorRecord, _ relation.Through[cursorRecord, cursorRecord]) cursorRecord { return r })
		for name, source := range map[string]AggregateSource[cursorRecord, cursorRecord]{"direct": testRelation(parent, target), "through": through, "pivot": through.Pivot()} {
			t.Run(name+map[bool]string{false: "_codec", true: "_metadata"}[registered], func(t *testing.T) {
				minimum := RelatedValue(source, Min(field))
				selected := SelectValue(parent, Coalesce(minimum, Param(id, int64(0))).Value())
				if _, err := selected.Compile(); err != nil {
					t.Fatal("valid relation expression rejected", err)
				}
				rejectSensitiveCursor(t, selected)
			})
		}
	}
	safe := SelectValue(parent, Coalesce(RelatedValue(testRelation(parent, parent), Min(id)), Param(id, int64(0))).Value())
	cursor := ValueCursorFor(safe)
	if _, err := cursor.UniqueBy(cursor.Key()).Compile(); err != nil {
		t.Fatal("public relation cursor rejected", err)
	}
}

func TestComputedCursorSensitivityWithoutModelFields(t *testing.T) {
	type row struct {
		ID       int64
		Name     string
		Optional value.Nullable[string]
	}
	base := ForModel(Define("bare", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "optional", Nullable: true}}, func(database.Row) (row, error) { return row{}, nil }))
	public := codec.String[string]()
	secret := public.WithSensitiveValues()
	field := NewScalarField[row, string]("bare", "name", secret)
	ordinary := NewScalarField[row, string]("bare", "name", public)
	optional := NewNullableField[row, string]("bare", "optional", secret)
	fallback := Param(ordinary, "public")
	for name, expression := range map[string]Expression[row, string]{
		"column comparison": When(field.EqColumn(ordinary), fallback).Else(Param(ordinary, "other")).Value(),
		"membership":        When(field.InQuery(SelectValue(base, fallback.Value())), fallback).Else(Param(ordinary, "other")).Value(),
		"lower":             Lower(field).Value(),
		"nested operations": Lower(Concat(ordinary, field)).Value(),
		"coalesce":          Coalesce(optional, fallback).Value(),
		"selected coalesce": CoalesceValue(Value(optional), fallback.Value()),
		"case result":       When(ordinary.Eq("x"), fallback).Else(field).Value(),
		"selected case":     WhenValue(Grouped(ordinary.Eq("x")), fallback.Value()).Else(Value(field)),
		"case condition":    When(field.Eq("x"), fallback).Else(Param(ordinary, "other")).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			selected := SelectValue(base, expression)
			if _, err := selected.Compile(); err != nil {
				t.Fatal("valid expression rejected", err)
			}
			rejectSensitiveCursor(t, selected)
		})
	}
	for name, expression := range map[string]Expression[row, value.Nullable[string]]{
		"nullable coalesce":          CoalesceNullable(NullableRow(fallback), optional).Value(),
		"selected nullable coalesce": CoalesceNullableValue(Nullable(fallback.Value()), Value(optional)),
		"nullif right":               NullIf(ordinary, field).Value(),
		"selected nullif":            NullIfValue(Value(ordinary), Value(field)),
	} {
		t.Run(name, func(t *testing.T) { rejectSensitiveCursor(t, SelectValue(base, expression)) })
	}
	rejectSensitiveCursor(t, SelectValue(base, RowNumber(WindowFor(base).OrderBy(field.Asc()))))
	rejectSensitiveCursor(t, SelectValue(base, RowNumber(WindowFor(base).PartitionBy(field.Group()))))
	lower := Lower(field)
	grouped := SelectValue(base, lower.Value()).GroupBy(lower.Group())
	if _, err := grouped.Compile(); err != nil {
		t.Fatal("annotation changed grouping identity", err)
	}
	rejectSensitiveCursor(t, grouped)
	// Aggregate result codecs can differ from their input codecs.
	number := NewExactField[row, int64]("bare", "id", codec.Signed[int64]().WithSensitiveValues())
	rejectSensitiveCursor(t, SelectValue(base, Sum(number).Value()))
	rejectSensitiveCursor(t, SelectValue(base, CountField(field).Value()))
	safe := ValueCursorFor(SelectValue(base, Lower(ordinary).Value()))
	if _, err := safe.UniqueBy(safe.Key()).Compile(); err != nil {
		t.Fatal("optional model getters became mandatory", err)
	}
}
