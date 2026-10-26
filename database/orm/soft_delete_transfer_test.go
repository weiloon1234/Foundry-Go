package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
	"time"
)

type transferSoft struct {
	ID        int64
	Name      string
	DeletedAt value.Nullable[time.Time]
	Child     relation.One[transferSoft]
	Links     relation.Through[transferSoft, transferSoft]
}
type transferSoftTable struct {
	Table[transferSoft, int64]
	Name      Text[transferSoft, string]
	DeletedAt SoftDelete[transferSoft, time.Time]
	Child     One[transferSoft, transferSoft]
	Links     Through[transferSoft, transferSoft, transferSoft]
}

func transferSoftFields() transferSoftTable {
	return Define("soft_records", func(b Builder[transferSoft], m *transferSoft) transferSoftTable {
		return transferSoftTable{TableOf(b, &m.ID), TextOf(b, &m.Name), SoftDeleteOf(b, &m.DeletedAt), OneOf(b, &m.Child), ThroughOf(b, &m.Links)}
	})
}
func TestSoftDeleteScopesPreserveExplicitFilters(t *testing.T) {
	d := transferSoftFields()
	base := d.Query().Where(Or(d.Key().Eq(1), d.Key().Eq(2)))
	for _, test := range []struct {
		q      Query[transferSoft]
		suffix string
	}{{base, `AND ("soft_records"."deleted_at" IS NULL)`}, {base.OnlyTrashed(), `AND ("soft_records"."deleted_at" IS NOT NULL)`}, {base.WithTrashed(), ""}, {base.OnlyTrashed().WithTrashed().WithoutTrashed(), `AND ("soft_records"."deleted_at" IS NULL)`}} {
		s, e := Compile(test.q)
		if e != nil || !strings.Contains(s.SQL(), `WHERE (("soft_records"."id" = $1) OR ("soft_records"."id" = $2))`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2)}) {
			t.Fatal(s.SQL(), e)
		}
		where := s.SQL()[strings.Index(s.SQL(), " WHERE "):]
		if test.suffix == "" && strings.Contains(where, "deleted_at") || test.suffix != "" && !strings.HasSuffix(where, test.suffix) {
			t.Fatal(where)
		}
	}
	s, e := Compile(base.Where(d.DeletedAt.IsNotNull()).WithTrashed())
	if e != nil || !strings.Contains(s.SQL(), `("soft_records"."deleted_at" IS NOT NULL)`) {
		t.Fatal(s.SQL(), e)
	}
	if base.state.ast.SoftDeleteScope != querycore.ActiveRecords || len(base.state.ast.Predicates) != 1 {
		t.Fatal("base mutated")
	}
}
func TestSoftDeleteRelationsScopeTargetsAndPivotsIndependently(t *testing.T) {
	d := transferSoftFields()
	one := HasOneOf(d.Child, d.Key(), d.Key())
	through := ManyToManyOf(d.Links, d.Key(), d.Key(), d.Key(), d.Key())
	for _, test := range []struct {
		p               Predicate[transferSoft]
		active, trashed int
	}{{Has(one), 2, 0}, {Has(one.WithTrashed()), 1, 0}, {Has(one.OnlyTrashed()), 1, 1}, {Has(through), 3, 0}, {Has(through.WithTrashed()), 2, 0}, {Has(through.WithTrashedPivot()), 2, 0}, {Has(through.OnlyTrashed().OnlyTrashedPivot()), 1, 2}, {Has(through.WithTrashed().WithTrashedPivot()), 1, 0}, {Has(through.OnlyTrashed().OnlyTrashedPivot().WithoutTrashed().WithoutTrashedPivot()), 3, 0}} {
		s, e := Compile(d.Query().Where(test.p))
		if e != nil || strings.Count(s.SQL(), " IS NULL") != test.active || strings.Count(s.SQL(), " IS NOT NULL") != test.trashed {
			t.Fatal(s.SQL(), e)
		}
	}
	for _, test := range []struct {
		input           aggregateInput
		active, trashed int
	}{{one.aggregateInput(nil, nil), 1, 0}, {one.OnlyTrashed().aggregateInput(nil, nil), 0, 1}, {through.aggregateInput(nil, nil), 2, 0}, {through.WithTrashed().OnlyTrashedPivot().aggregateInput(nil, nil), 0, 1}} {
		n, e := aggregateSelect(&aggregatePlan{input: test.input, measure: querycore.AggregateNode{Kind: querycore.CountAll}}, t.Context(), []driver.Value{int64(3)}, 5)
		if e != nil {
			t.Fatal(e)
		}
		s, e := querycore.CompileRead(n, t.Context(), false, nil, true)
		if e != nil || strings.Count(s.SQLText, " IS NULL") != test.active || strings.Count(s.SQLText, " IS NOT NULL") != test.trashed {
			t.Fatal("aggregate input scope", s.SQLText, e)
		}
	}
	captured := one.WithTrashed()
	if e := Check(d, captured); e != nil || captured.plan.query.ast.SoftDeleteScope != querycore.AllRecords {
		t.Fatal("descriptor visibility lost", e)
	}
}
func TestSoftDeleteMetadataAndUnsupportedScopeFailExplicitly(t *testing.T) {
	plain := scopeTransferFields()
	for _, q := range []Query[scopeTransferRow]{plain.Query().WithTrashed(), plain.Query().OnlyTrashed(), Query[scopeTransferRow]{}.WithTrashed()} {
		if _, e := Compile(q); !errors.Is(e, fault.Invalid) {
			t.Fatal("unsupported visibility", e)
		}
	}
	for _, mode := range []string{"nil", "foreign"} {
		bad := Define("bad_soft", func(b Builder[transferSoft], m *transferSoft) Table[transferSoft, int64] {
			TextOf(b, &m.Name)
			OneOf(b, &m.Child)
			ThroughOf(b, &m.Links)
			var ptr *value.Nullable[time.Time]
			if mode == "foreign" {
				ptr = new(value.Nullable[time.Time])
			}
			SoftDeleteOf(b, ptr)
			return TableOf(b, &m.ID)
		})
		if e := Check(bad); !errors.Is(e, fault.Invalid) {
			t.Fatal(mode, e)
		}
	}
	type wrong struct {
		ID        int64
		DeletedAt string
	}
	bad := Define("wrong_soft", func(b Builder[wrong], m *wrong) Table[wrong, int64] {
		TextOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	})
	if e := Check(bad); !errors.Is(e, fault.Invalid) {
		t.Fatal("noninstant deletion", e)
	}

}
func TestAutomaticRolesRejectDistinctInputsWithoutCallbacks(t *testing.T) {
	calls := 0
	type row struct {
		ID        int64
		CreatedAt time.Time
		UpdatedAt time.Time
		DeletedAt value.Nullable[time.Time]
	}
	for _, kind := range []string{"created", "deleted"} {
		d := Define("invalid_automatic", func(b Builder[row], m *row) Table[row, int64] {
			if kind == "created" {
				InputTimeOf(b, &m.CreatedAt, func(string) (time.Time, error) { calls++; return time.Time{}, nil })
			} else {
				CreatedAtOf(b, &m.CreatedAt)
			}
			UpdatedAtOf(b, &m.UpdatedAt)
			if kind == "deleted" {
				NullableInputTimeOf(b, &m.DeletedAt, func(string) (time.Time, error) { calls++; return time.Time{}, nil })
			} else {
				SoftDeleteOf(b, &m.DeletedAt)
			}
			return TableOf(b, &m.ID)
		})
		for range 2 {
			if e := Check(d); !errors.Is(e, fault.Invalid) {
				t.Fatal(kind, e)
			}
		}
	}
	type nullable struct {
		ID        int64
		CreatedAt value.Nullable[time.Time]
		UpdatedAt time.Time
	}
	bad := Define("nullable_managed", func(b Builder[nullable], m *nullable) Table[nullable, int64] {
		NullableTimeOf(b, &m.CreatedAt)
		UpdatedAtOf(b, &m.UpdatedAt)
		return TableOf(b, &m.ID)
	})
	if e := Check(bad); !errors.Is(e, fault.Invalid) {
		t.Fatal("nullable timestamp", e)
	}
	for _, kind := range []string{"nil pointer", "zero codec", "duplicate role"} {
		bad := Define("bad_timestamp", func(b Builder[row], m *row) Table[row, int64] {
			switch kind {
			case "nil pointer":
				CreatedAtOf(b, (*time.Time)(nil))
			case "zero codec":
				ColumnOf(b, &m.CreatedAt, Codec(codec.Codec[time.Time]{}))
			case "duplicate role":
				CreatedAtOf(b, &m.CreatedAt)
				CreatedAtOf(b, &m.UpdatedAt)
			}
			UpdatedAtOf(b, &m.UpdatedAt)
			SoftDeleteOf(b, &m.DeletedAt)
			return TableOf(b, &m.ID)
		})
		if e := Check(bad); !errors.Is(e, fault.Invalid) {
			t.Fatal(kind, e)
		}
	}
	if calls != 0 {
		t.Fatal("metadata invoked transform", calls)
	}
}
