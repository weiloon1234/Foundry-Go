package query

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type softRecord struct {
	id      int64
	name    string
	deleted value.Nullable[time.Time]
}

func softQuery() Query[softRecord] {
	return ForModel(Define("soft_records", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "deleted_on", Nullable: true}},
		func(row database.Row) (softRecord, error) {
			var r softRecord
			err := row.Scan(&r.id, &r.name, codec.Nullable(codec.Time()).Scan(&r.deleted))
			return r, err
		},
		NewModelField("id", codec.Signed[int64](), func(r softRecord) int64 { return r.id }),
		NewModelField("name", codec.String[string](), func(r softRecord) string { return r.name }),
		NewModelField("deleted_on", codec.Nullable(codec.Time()), func(r softRecord) value.Nullable[time.Time] { return r.deleted }),
	).WithSoftDeletes("deleted_on"))
}

func softID() ScalarField[softRecord, int64] {
	return NewScalarField[softRecord, int64]("soft_records", "id", codec.Signed[int64]())
}

func softOne() OneRelation[softRecord, softRecord] {
	q, id := softQuery(), softID()
	return HasOne(id, id).Bind("Child", q, q, func(softRecord) relation.One[softRecord] { return relation.One[softRecord]{} }, func(r softRecord, _ relation.One[softRecord]) softRecord { return r })
}
func softThrough() ThroughRelation[softRecord, softRecord, softRecord] {
	q, id := softQuery(), softID()
	return ManyToMany(id, id, id, id).Bind("Links", q, q, q, func(softRecord) relation.Through[softRecord, softRecord] {
		return relation.Through[softRecord, softRecord]{}
	}, func(r softRecord, _ relation.Through[softRecord, softRecord]) softRecord { return r })
}

func TestSoftDeleteScopesPreserveExplicitFilters(t *testing.T) {
	id := softID()
	base := softQuery().Where(Or(id.Eq(1), id.Eq(2)))
	for _, test := range []struct {
		q      Query[softRecord]
		suffix string
	}{
		{base, `AND ("soft_records"."deleted_on" IS NULL)`},
		{base.OnlyTrashed(), `AND ("soft_records"."deleted_on" IS NOT NULL)`},
		{base.WithTrashed(), ""},
		{base.OnlyTrashed().WithTrashed().WithoutTrashed(), `AND ("soft_records"."deleted_on" IS NULL)`},
	} {
		statement, err := test.q.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(statement.SQL(), `WHERE (("soft_records"."id" = $1) OR ("soft_records"."id" = $2))`) || !reflect.DeepEqual(statement.Arguments(), []any{int64(1), int64(2)}) {
			t.Fatal("visibility replaced explicit filters or binding order", statement.SQL())
		}
		where := statement.SQL()[strings.Index(statement.SQL(), " WHERE "):]
		if test.suffix == "" && strings.Contains(where, "deleted_on") || test.suffix != "" && !strings.HasSuffix(where, test.suffix) {
			t.Fatal("wrong deletion visibility", where)
		}
	}
	deleted := NewNullableOrderedField[softRecord, time.Time]("soft_records", "deleted_on", codec.Time())
	explicit, err := base.Where(deleted.IsNotNull()).WithTrashed().Compile()
	if err != nil || !strings.Contains(explicit.SQL(), `("soft_records"."deleted_on" IS NOT NULL)`) {
		t.Fatal("including trashed removed an explicit filter", err)
	}
	if base.softDeleteScope != activeRecords || len(base.predicates) != 1 {
		t.Fatal("derivation mutated the reusable base query")
	}
}

func TestSoftDeleteScopesReachAliasesAndModelMutations(t *testing.T) {
	q, id := softQuery(), softID()
	for _, source := range []Query[softRecord]{q, q.WithTrashed(), q.OnlyTrashed()} {
		alias := As[firstAlias](source, "member")
		statement, err := SelectRecord(alias, alias.Scope()).Compile()
		if err != nil {
			t.Fatal(err)
		}
		filtered := source.softDeleteScope != allRecords
		if strings.Contains(statement.SQL(), `"deleted_on" IS `) != filtered {
			t.Fatal("alias dropped default scope", statement.SQL())
		}
		for _, kind := range []mutationKind{updateModel, deleteModel} {
			plan := mutationPlan[softRecord]{query: source.Where(id.Eq(7)), kind: kind}
			if kind == updateModel {
				plan.mutation = Change(Assign[softRecord]("soft_records", "name", codec.String[string](), "changed"))
			}
			statement, err = plan.compile()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(statement.SQL(), `"deleted_on" IS `) != filtered {
				t.Fatal("mutation dropped model scope", statement.SQL())
			}
		}
	}
	for _, test := range []struct {
		q       LockedQuery[softRecord]
		suffix  string
		trashed bool
	}{
		{q.ForUpdate().WithTrashed(), "FOR UPDATE", false},
		{q.ForShare().OnlyTrashed().NoWait(), "FOR SHARE NOWAIT", true},
	} {
		statement, err := test.q.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(statement.SQL(), test.suffix) || strings.Contains(statement.SQL(), " IS NOT NULL") != test.trashed || strings.Contains(statement.SQL(), " IS NULL") {
			t.Fatal("visibility derivation lost the row-lock policy", statement.SQL())
		}
	}
	if _, err := (mutationPlan[softRecord]{query: q, kind: deleteModel}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("implicit scope substituted for required key equality", err)
	}
}

func TestSoftDeleteRelationsScopeTargetsAndPivotsIndependently(t *testing.T) {
	q := softQuery()
	for _, test := range []struct {
		relation        ExistenceRelation[softRecord]
		active, trashed int
	}{
		{softOne(), 2, 0}, {softOne().WithTrashed(), 1, 0}, {softOne().OnlyTrashed(), 1, 1},
		{softThrough(), 3, 0}, {softThrough().WithTrashed(), 2, 0},
		{softThrough().WithTrashedPivot(), 2, 0},
		{softThrough().OnlyTrashed().OnlyTrashedPivot(), 1, 2},
		{softThrough().WithTrashed().WithTrashedPivot(), 1, 0},
		{softThrough().OnlyTrashed().OnlyTrashedPivot().WithoutTrashed().WithoutTrashedPivot(), 3, 0},
	} {
		statement, err := q.WhereHas(test.relation).Compile()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(statement.SQL(), " IS NULL") != test.active || strings.Count(statement.SQL(), " IS NOT NULL") != test.trashed {
			t.Fatal("relation existence lost independent scopes", statement.SQL())
		}
	}
	for _, test := range []struct {
		input           aggregateInput[softRecord, softRecord]
		active, trashed int
	}{
		{softOne().aggregateInput(), 1, 0}, {softOne().OnlyTrashed().aggregateInput(), 0, 1},
		{softThrough().aggregateInput(), 2, 0}, {softThrough().WithTrashed().OnlyTrashedPivot().aggregateInput(), 0, 1},
	} {
		statement, err := compileRelationAggregate(t.Context(), test.input, Count[softRecord]().node, []driver.Value{int64(3)}, 5)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(statement.SQL(), " IS NULL") != test.active || strings.Count(statement.SQL(), " IS NOT NULL") != test.trashed {
			t.Fatal("relation aggregate lost deletion scopes", statement.SQL())
		}
	}
	relation := softOne().WithTrashed()
	rebound := relation.Bind("Child", q, q, relation.get, relation.set)
	if err := q.With(rebound).Validate(); err != nil {
		t.Fatal(err)
	}
	if rebound.spec.target.softDeleteScope != allRecords {
		t.Fatal("binding discarded descriptor visibility")
	}
	invalid := relation.Bind("Child", q, q.WithTrashed(), relation.get, relation.set)
	if err := q.With(invalid).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("binding silently accepted scoped metadata", err)
	}
}

func TestSoftDeleteMetadataAndUnsupportedScopeFailExplicitly(t *testing.T) {
	q := softQuery()
	for _, column := range []string{"", "missing", "id", "name"} {
		d := q.definition.WithSoftDeletes(column)
		if err := d.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid deletion metadata accepted", column, err)
		}
	}
	plain := *q.definition
	plain.softDelete = nil
	for _, query := range []Query[softRecord]{ForModel(plain).WithTrashed(), ForModel(plain).OnlyTrashed(), For[softRecord]("soft_records").WithTrashed()} {
		if _, err := query.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsupported scope accepted", err)
		}
		if source := query.projectionSource(); !errors.Is(source.err, fault.Invalid) {
			t.Fatal("projection discarded unsupported visibility", source.err)
		}
	}
	d := *q.definition
	d.modelFields = append([]ModelField[softRecord](nil), d.modelFields...)
	d.modelFields[2] = NewNullableInputModelField("deleted_on", codec.Time(), func(r softRecord) value.Nullable[time.Time] { return r.deleted }, func(string) (time.Time, error) { return time.Time{}, nil })
	if err := d.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("distinct automatic deletion input accepted", err)
	}
}
