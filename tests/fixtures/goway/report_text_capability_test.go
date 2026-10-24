package goway_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/goway/models"
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// The reviewer's construction: the shallow enum supplies metadata while a
// zero-valued, deeper text handle supplies the framework's private marker.
type donor struct {
	orm.Text[models.User, models.Status]
}
type forged struct {
	orm.Enum[models.User, models.Status]
	donor
}

func (forged) Contains(models.Status) orm.Predicate[models.User] {
	return orm.Predicate[models.User]{}
}

type reportNullableEnum struct {
	ID    int64
	State value.Nullable[models.Status]
}
type reportNullableEnumFields struct {
	orm.Table[reportNullableEnum, int64]
	State orm.NullableEnum[reportNullableEnum, models.Status]
}
type nullableDonor struct {
	orm.NullableText[reportNullableEnum, models.Status]
}
type nullableForged struct {
	orm.NullableEnum[reportNullableEnum, models.Status]
	nullableDonor
}

func (nullableForged) Contains(models.Status) orm.Predicate[reportNullableEnum] {
	panic("adapter must not invoke an application Contains method")
}

func TestReportTextNestedEmbedding(t *testing.T) {
	input := forged{Enum: models.Users.Status}
	// Keep the exact reviewed call, including explicit type arguments and Gt.
	p := expr.TextRow[models.User, models.Status](input).Gt(models.StatusActive)
	_, err := rowset.Compile(rowset.FromModel(models.Users.Query()).Where(p))
	assertReportTextRejected(t, err)
	for name, column := range map[string]orm.TextExpressionColumn[models.User, models.Status, models.Status]{"value": input, "pointer": &input} {
		t.Run(name, func(t *testing.T) {
			text := expr.TextRow(column)
			assertReportTextOperationsRejected(t, models.Users.Query(), text)
			t.Run("scoped", func(t *testing.T) {
				assertReportTextOperationsRejected(t, models.Users.Query(), expr.TextRowAt(rowset.ModelScope(models.Users.Query()), column))
			})
			q := rowset.FromModel(models.Users.Query())
			checks := map[string]expr.Predicate[models.User]{
				"selected":       rowset.ExistsQuery(q, rowset.SelectScalar(q, text.Value())),
				"param":          text.Param(models.StatusActive).Eq(models.StatusActive),
				"selected_param": text.Value().Param(models.StatusActive).Eq(models.StatusActive),
				"nullable":       expr.NullableRow(text).Eq(value.Of(models.StatusActive)),
				"computed":       expr.Lower(text).Gt("active"),
				"not":            expr.Not(text.Gt(models.StatusActive)),
				"or":             expr.Or(text.Eq(models.StatusActive), expr.ModelPredicate(models.Users.Age.Gt(1))),
			}
			for name, p := range checks {
				t.Run(name, func(t *testing.T) { _, err := rowset.Compile(q.Where(p)); assertReportTextRejected(t, err) })
			}
			_, err := rowset.Compile(rowset.GroupBy(q, expr.Group(text)))
			assertReportTextRejected(t, err)
			_, err = rowset.Compile(rowset.DistinctOnValues(q, text.Value().Key()))
			assertReportTextRejected(t, err)
		})
	}
}

func TestReportNullableTextNestedEmbedding(t *testing.T) {
	f := orm.Define("report_nullable_enums", func(b orm.Builder[reportNullableEnum], r *reportNullableEnum) reportNullableEnumFields {
		return reportNullableEnumFields{orm.TableOf(b, &r.ID), orm.NullableEnumOf(b, &r.State)}
	})
	input := nullableForged{NullableEnum: f.State}
	for name, column := range map[string]orm.TextExpressionColumn[reportNullableEnum, models.Status, value.Nullable[models.Status]]{"value": input, "pointer": &input} {
		t.Run(name, func(t *testing.T) {
			text := expr.TextNullableRow(column)
			assertReportTextOperationsRejected(t, f.Query(), text)
			t.Run("scoped", func(t *testing.T) {
				assertReportTextOperationsRejected(t, f.Query(), expr.TextNullableRowAt(rowset.ModelScope(f.Query()), column))
			})
			q := rowset.FromModel(f.Query())
			checks := map[string]expr.Predicate[reportNullableEnum]{
				"selected":       rowset.ExistsQuery(q, rowset.SelectScalar(q, text.Value())),
				"param":          text.Param(value.Of(models.StatusActive)).Eq(value.Of(models.StatusActive)),
				"selected_param": text.Value().Param(value.Of(models.StatusActive)).Eq(value.Of(models.StatusActive)),
				"null_param":     text.Param(value.Null[models.Status]()).IsNull(),
				"computed":       expr.LowerNullable(text).Gt("active"),
				"not":            expr.Not(text.Gt(models.StatusActive)),
				"or":             expr.Or(text.Eq(models.StatusActive), expr.ModelPredicate(f.State.IsNull())),
			}
			for name, p := range checks {
				t.Run(name, func(t *testing.T) { _, err := rowset.Compile(q.Where(p)); assertReportTextRejected(t, err) })
			}
			_, err := rowset.Compile(rowset.GroupBy(q, expr.Group(text)))
			assertReportTextRejected(t, err)
			_, err = rowset.Compile(rowset.DistinctOnValues(q, text.Value().Key()))
			assertReportTextRejected(t, err)
		})
	}
}

// Both wrappers expose these operations with the same present enum value type.
type reportTextOperations[M any] interface {
	Eq(models.Status) expr.Predicate[M]
	Ne(models.Status) expr.Predicate[M]
	In(...models.Status) expr.Predicate[M]
	Lt(models.Status) expr.Predicate[M]
	Lte(models.Status) expr.Predicate[M]
	Gt(models.Status) expr.Predicate[M]
	Gte(models.Status) expr.Predicate[M]
	IsNull() expr.Predicate[M]
	IsNotNull() expr.Predicate[M]
	Like(models.Status) expr.Predicate[M]
	ILike(models.Status) expr.Predicate[M]
	Contains(models.Status) expr.Predicate[M]
	IContains(models.Status) expr.Predicate[M]
	StartsWith(models.Status) expr.Predicate[M]
	EndsWith(models.Status) expr.Predicate[M]
	IStartsWith(models.Status) expr.Predicate[M]
	IEndsWith(models.Status) expr.Predicate[M]
	Asc() expr.Order[M]
	Desc() expr.Order[M]
}

func assertReportTextRejected(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "text adapter requires a text column") {
		t.Fatalf("expected adapter capability error, got %v", err)
	}
}

func assertReportTextOperationsRejected[M any](t *testing.T, source orm.Query[M], text reportTextOperations[M]) {
	t.Helper()
	q := rowset.FromModel(source)
	v := models.StatusActive
	for name, p := range map[string]expr.Predicate[M]{
		"eq": text.Eq(v), "ne": text.Ne(v), "in": text.In(v), "empty_in": text.In(),
		"lt": text.Lt(v), "lte": text.Lte(v), "gt": text.Gt(v), "gte": text.Gte(v),
		"null": text.IsNull(), "not_null": text.IsNotNull(),
		"like": text.Like(v), "ilike": text.ILike(v), "contains": text.Contains(v), "icontains": text.IContains(v),
		"starts": text.StartsWith(v), "ends": text.EndsWith(v), "istarts": text.IStartsWith(v), "iends": text.IEndsWith(v),
	} {
		t.Run(name, func(t *testing.T) { _, err := rowset.Compile(q.Where(p)); assertReportTextRejected(t, err) })
	}
	for name, order := range map[string]expr.Order[M]{"asc": text.Asc(), "desc": text.Desc(), "nulls_first": text.Asc().NullsFirst(), "nulls_last": text.Desc().NullsLast()} {
		t.Run(name, func(t *testing.T) {
			_, err := rowset.Compile(rowset.OrderBy(q, order))
			assertReportTextRejected(t, err)
		})
	}
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	bad := q.Where(text.Gt(v))
	_, err := rowset.Count(t.Context(), db, bad)
	assertReportTextRejected(t, err)
	called := false
	err = rowset.Each(t.Context(), db, bad, func(M) error { called = true; return nil })
	assertReportTextRejected(t, err)
	if capture.statements != 0 || called {
		t.Fatal("invalid adapter reached SQL or yield callback")
	}
}

func TestReportTextSupportedFamilies(t *testing.T) {
	ordinary := rowset.FromModel(models.Users.Query()).Where(expr.TextRow(models.Users.Email).Gt("ada@example.test"), expr.TextNullableRow(models.Users.Nickname).Contains("Ada"))
	if _, err := rowset.Compile(ordinary); err != nil {
		t.Fatal(err)
	}
	input := rowset.FromModel(mutators.Members.Query()).Where(expr.TextRow(mutators.Members.Email).Gt("ada@example.test"), expr.TextNullableRow(mutators.Members.Note).Contains("Ada"))
	if _, err := rowset.Compile(input); err != nil {
		t.Fatal(err)
	}
}

// A shallower Relation can similarly override the plan while a donor supplies
// a different private target-model marker.
type reportRelationDonor struct {
	orm.BelongsTo[models.User, models.User]
}
type reportForgedRelation struct {
	orm.Relation[models.User]
	reportRelationDonor
}

func TestReportRelationNestedTargetMarker(t *testing.T) {
	r := reportForgedRelation{Relation: models.UserRelations.Orders}
	q := rowset.FromModel(models.Users.Query())
	for _, ps := range [][]expr.Predicate[models.User]{nil, {expr.ModelPredicate(orm.WhereRaw[models.User]("TRUE"))}} {
		_, err := rowset.Compile(q.Where(expr.Has[models.User, models.User](r, ps...)))
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "relation model types do not match bound metadata") {
			t.Fatalf("promoted relation target accepted: %v", err)
		}
	}
	// Embedding does not itself invalidate a relation when its own plan agrees.
	r.Relation = models.UserRelations.Introducer
	if _, err := rowset.Compile(q.Where(expr.Has[models.User, models.User](r))); err != nil {
		t.Fatal(err)
	}
}

func TestReportModelPredicateNestedRelationTargetMarker(t *testing.T) {
	q := rowset.FromModel(models.Users.Query())
	for _, matching := range []bool{false, true} {
		r := reportForgedRelation{Relation: models.UserRelations.Orders}
		if matching {
			r.Relation = models.UserRelations.Introducer
		}
		for _, relation := range []orm.ExistenceRelation[models.User, models.User]{r, &r} {
			for name, ps := range map[string][]orm.Predicate[models.User]{
				"no child":             nil,
				"reference-free child": {orm.WhereRaw[models.User]("TRUE")},
				"model child":          {models.Users.Age.Gt(0)},
			} {
				for operation, predicate := range map[string]orm.Predicate[models.User]{
					"Has":        orm.Has[models.User, models.User](relation, ps...),
					"DoesntHave": orm.DoesntHave[models.User, models.User](relation, ps...),
				} {
					_, err := rowset.Compile(q.Where(expr.ModelPredicate(predicate)))
					if matching {
						if err != nil {
							t.Fatalf("matching %s (%s): %v", operation, name, err)
						}
					} else if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "relation model types do not match bound metadata") {
						t.Fatalf("forged %s (%s) accepted: %v", operation, name, err)
					}
				}
			}
		}
	}
}
