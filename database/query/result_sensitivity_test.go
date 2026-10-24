package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestResultCursorCompositionPreservesSensitivity(t *testing.T) {
	type row struct {
		ID   int64
		Name string
	}
	publicCodec, secretCodec := codec.String[string](), codec.String[string]().WithSensitiveValues()
	base := ForModel(Define("secrets", "id", []Column{{Name: "id"}, {Name: "name"}}, func(database.Row) (row, error) { return row{}, nil }, NewModelField("id", codec.Signed[int64](), func(r row) int64 { return r.ID }), NewModelField("name", secretCodec, func(r row) string { return r.Name })))
	id := NewOrderedField[row, int64]("secrets", "id", codec.Signed[int64]())
	name := NewScalarField[row, string]("secrets", "name", secretCodec)
	// The public input is a constant, independent of the sensitive source column.
	public := SelectValue(base, Value(Param(NewScalarField[row, string]("secrets", "name", publicCodec), "public")))
	secret := SelectValue(base, Value(name))
	for label, source := range map[string]ValueQuerySource[string]{"right": public.UnionAll(secret), "left": secret.UnionAll(public), "nested": public.UnionAll(public.UnionAll(secret)), "intersect": public.Intersect(secret), "except": public.Except(secret), "computed": SelectValue(base, Lower(name).Value())} {
		t.Run(label, func(t *testing.T) {
			c := ValueCursorFor(source)
			if _, err := c.UniqueBy(c.Key()).Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("sensitive cursor compiled", err)
			}
		})
	}
	outputID, outputName := NewProjectionField[row, int64]("id"), NewProjectionField[row, string]("name")
	definition := DefineProjection([]ProjectionColumn[row]{outputID.Column(), outputName.Column()}, func(database.Row) (row, error) { return row{}, nil }, NewRecordField("id", codec.Signed[int64](), func(r row) int64 { return r.ID }), NewRecordField("name", publicCodec, func(r row) string { return r.Name }))
	report := Project(base, definition, Map(outputID, Value(id)), Map(outputName, Value(name)))
	ordinary := Project(base, definition, Map(outputID, Value(id)), Map(outputName, Value(Param(NewScalarField[row, string]("secrets", "name", publicCodec), "public"))))
	for label, source := range map[string]RecordQuerySource[row]{"projection": report, "right record": UnionAll(ordinary, report), "left record": UnionAll(report, ordinary), "CTE": CTE("sensitive_report", report)} {
		t.Run(label, func(t *testing.T) {
			c := CursorFor(source)
			key := NewScalarField[CursorScope[row], string](c.Scope().table, "name", publicCodec)
			if _, err := c.UniqueBy(key.Group()).Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("mapped secret compiled", err)
			}
			safe := NewOrderedField[CursorScope[row], int64](c.Scope().table, "id", codec.Signed[int64]())
			if _, err := c.UniqueBy(safe.Group()).Compile(); err != nil {
				t.Fatal("public output rejected", err)
			}
			type tag struct{}
			a := As[tag](source, "report_alias")
			field := NewScalarField[Alias[tag, row], string](a.Scope().table, "name", publicCodec)
			scalar := ValueCursorFor(SelectValue(a, Value(field)))
			if _, err := scalar.UniqueBy(scalar.Key()).Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("alias/scalar erased sensitivity", err)
			}
		})
	}
	set := public.UnionAll(secret)
	selected := ValueCursorFor(SelectValue(set, set.Value()))
	if _, err := selected.UniqueBy(selected.Key()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("set/scalar erased sensitivity", err)
	}
	clean := CursorFor(ordinary)
	if _, err := clean.UniqueBy(NewScalarField[CursorScope[row], string](clean.Scope().table, "name", publicCodec).Group()).Compile(); err != nil {
		t.Fatal("shared declaration mutated", err)
	}
}
