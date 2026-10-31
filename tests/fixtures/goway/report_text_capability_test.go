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

// The shallow enum adapter supplies metadata while a deeper text witness
// supplies the framework's private marker. The nil witness must never be called.
type donor struct {
	orm.TextExpressionColumn[models.User, models.Status, models.Status]
}
type forged struct {
	orm.EnumAdapter[models.User, models.Status]
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
	orm.NullableTextAdapter[reportNullableEnum, models.Status]
}
type nullableForged struct {
	orm.NullableEnumAdapter[reportNullableEnum, models.Status]
	nullableDonor
}

func (nullableForged) Contains(models.Status) orm.Predicate[reportNullableEnum] {
	panic("adapter must not invoke an application Contains method")
}

func TestReportTextNestedEmbedding(t *testing.T) {
	input := forged{EnumAdapter: orm.EnumExpr(models.Users.Status)}
	// Keep the exact reviewed call, including explicit type arguments and Gt.
	p := expr.Gt(expr.TextRow[models.User, models.Status](input), models.StatusActive)
	_, err := rowset.CompileRead(rowset.Read(rowset.FromModel(models.Users.Query()).Where(p)))
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
				"selected":       rowset.ExistsQuery(q, rowset.SelectScalar(q, expr.AsSelected(text))),
				"param":          expr.Eq(expr.ParamOf(text, models.StatusActive), models.StatusActive),
				"selected_param": expr.Eq(expr.ParamOf(expr.AsSelected(text), models.StatusActive), models.StatusActive),
				"nullable":       expr.Eq(expr.NullableRow(text), value.Of(models.StatusActive)),
				"computed":       expr.Gt(expr.Lower(text), "active"),
				"not":            expr.Not(expr.Gt(text, models.StatusActive)),
				"or":             expr.Or(expr.Eq(text, models.StatusActive), expr.ModelPredicate(orm.NumberGt(models.Users.Age, 1))),
			}
			for name, p := range checks {
				t.Run(name, func(t *testing.T) {
					_, err := rowset.CompileRead(rowset.Read(q.Where(p)))
					assertReportTextRejected(t, err)
				})
			}
			_, err := rowset.CompileRead(rowset.Read(rowset.GroupBy(q, expr.Group(text))))
			assertReportTextRejected(t, err)
			_, err = rowset.CompileRead(rowset.Read(rowset.DistinctOnValues(q, expr.Key(expr.AsSelected(text)))))
			assertReportTextRejected(t, err)
		})
	}
}

func TestReportNullableTextNestedEmbedding(t *testing.T) {
	f := orm.Define("report_nullable_enums", func(b orm.Builder[reportNullableEnum], r *reportNullableEnum) reportNullableEnumFields {
		return reportNullableEnumFields{orm.TableOf(b, &r.ID), orm.NullableEnumOf(b, &r.State)}
	})
	input := nullableForged{NullableEnumAdapter: orm.NullableEnumExpr(f.State)}
	for name, column := range map[string]orm.TextExpressionColumn[reportNullableEnum, models.Status, value.Nullable[models.Status]]{"value": input, "pointer": &input} {
		t.Run(name, func(t *testing.T) {
			text := expr.TextNullableRow(column)
			assertReportNullableTextOperationsRejected(t, f.Query(), text)
			t.Run("scoped", func(t *testing.T) {
				assertReportNullableTextOperationsRejected(t, f.Query(), expr.TextNullableRowAt(rowset.ModelScope(f.Query()), column))
			})
			q := rowset.FromModel(f.Query())
			checks := map[string]expr.Predicate[reportNullableEnum]{
				"selected":       rowset.ExistsQuery(q, rowset.SelectScalar(q, expr.AsSelected(text))),
				"param":          expr.Eq(expr.ParamOf(text, value.Of(models.StatusActive)), value.Of(models.StatusActive)),
				"selected_param": expr.Eq(expr.ParamOf(expr.AsSelected(text), value.Of(models.StatusActive)), value.Of(models.StatusActive)),
				"null_param":     expr.IsNull(expr.ParamOf(text, value.Null[models.Status]())),
				"computed":       expr.NullableGt(expr.LowerNullable(text), "active"),
				"not":            expr.Not(expr.NullableGt(text, models.StatusActive)),
				"or":             expr.Or(expr.NullableEq(text, models.StatusActive), expr.ModelPredicate(orm.NullableEnumIsNull(f.State))),
			}
			for name, p := range checks {
				t.Run(name, func(t *testing.T) {
					_, err := rowset.CompileRead(rowset.Read(q.Where(p)))
					assertReportTextRejected(t, err)
				})
			}
			_, err := rowset.CompileRead(rowset.Read(rowset.GroupBy(q, expr.Group(text))))
			assertReportTextRejected(t, err)
			_, err = rowset.CompileRead(rowset.Read(rowset.DistinctOnValues(q, expr.Key(expr.AsSelected(text)))))
			assertReportTextRejected(t, err)
		})
	}
}

func assertReportTextRejected(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "text adapter requires a text column") {
		t.Fatalf("expected adapter capability error, got %v", err)
	}
}

func assertReportTextOperationsRejected[M any](t *testing.T, source orm.Query[M], text expr.TextRowExpression[M, models.Status]) {
	t.Helper()
	v := models.StatusActive
	assertReportTextPredicatesRejected(t, source, map[string]expr.Predicate[M]{
		"eq": expr.Eq(text, v), "ne": expr.Ne(text, v), "in": expr.In(text, v), "empty_in": expr.In(text),
		"lt": expr.Lt(text, v), "lte": expr.Lte(text, v), "gt": expr.Gt(text, v), "gte": expr.Gte(text, v),
		"null": expr.IsNull(text), "not_null": expr.IsNotNull(text),
		"like": expr.TextLike(text, v), "ilike": expr.TextILike(text, v), "contains": expr.TextContains(text, v), "icontains": expr.TextIContains(text, v),
		"starts": expr.TextStartsWith(text, v), "ends": expr.TextEndsWith(text, v), "istarts": expr.TextIStartsWith(text, v), "iends": expr.TextIEndsWith(text, v),
	}, map[string]expr.Order[M]{"asc": expr.Asc(text), "desc": expr.Desc(text), "nulls_first": expr.Asc(text).NullsFirst(), "nulls_last": expr.Desc(text).NullsLast()})
}

func assertReportNullableTextOperationsRejected[M any](t *testing.T, source orm.Query[M], text expr.NullableTextRowExpression[M, models.Status]) {
	t.Helper()
	v := models.StatusActive
	assertReportTextPredicatesRejected(t, source, map[string]expr.Predicate[M]{
		"eq": expr.NullableEq(text, v), "ne": expr.NullableNe(text, v), "in": expr.NullableIn(text, v), "empty_in": expr.NullableIn(text),
		"lt": expr.NullableLt(text, v), "lte": expr.NullableLte(text, v), "gt": expr.NullableGt(text, v), "gte": expr.NullableGte(text, v),
		"null": expr.IsNull(text), "not_null": expr.IsNotNull(text),
		"like": expr.NullableTextLike(text, v), "ilike": expr.NullableTextILike(text, v), "contains": expr.NullableTextContains(text, v), "icontains": expr.NullableTextIContains(text, v),
		"starts": expr.NullableTextStartsWith(text, v), "ends": expr.NullableTextEndsWith(text, v), "istarts": expr.NullableTextIStartsWith(text, v), "iends": expr.NullableTextIEndsWith(text, v),
	}, map[string]expr.Order[M]{"asc": expr.Asc(text), "desc": expr.Desc(text), "nulls_first": expr.Asc(text).NullsFirst(), "nulls_last": expr.Desc(text).NullsLast()})
}

func assertReportTextPredicatesRejected[M any](t *testing.T, source orm.Query[M], predicates map[string]expr.Predicate[M], orders map[string]expr.Order[M]) {
	t.Helper()
	q := rowset.FromModel(source)
	for name, p := range predicates {
		t.Run(name, func(t *testing.T) {
			_, err := rowset.CompileRead(rowset.Read(q.Where(p)))
			assertReportTextRejected(t, err)
		})
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			_, err := rowset.CompileRead(rowset.Read(rowset.OrderBy(q, order)))
			assertReportTextRejected(t, err)
		})
	}
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	bad := q.Where(predicates["gt"])
	_, err := rowset.Read(bad).Count(t.Context(), db)
	assertReportTextRejected(t, err)
	called := false
	err = rowset.EachRead(t.Context(), db, rowset.Read(bad), func(M) error { called = true; return nil })
	assertReportTextRejected(t, err)
	if capture.statements != 0 || called {
		t.Fatal("invalid adapter reached SQL or yield callback")
	}
}

func TestReportTextSupportedFamilies(t *testing.T) {
	ordinary := rowset.FromModel(models.Users.Query()).Where(expr.Gt(expr.TextRow(orm.TextExpr(models.Users.Email)), "ada@example.test"), expr.NullableTextContains(expr.TextNullableRow(orm.NullableTextExpr(models.Users.Nickname)), "Ada"))
	if _, err := rowset.CompileRead(rowset.Read(ordinary)); err != nil {
		t.Fatal(err)
	}
	input := rowset.FromModel(mutators.Members.Query()).Where(expr.Gt(expr.TextRow(orm.InputTextExpr(mutators.Members.Email)), "ada@example.test"), expr.NullableTextContains(expr.TextNullableRow(orm.NullableInputTextExpr(mutators.Members.Note)), "Ada"))
	if _, err := rowset.CompileRead(rowset.Read(input)); err != nil {
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
		_, err := rowset.CompileRead(rowset.Read(q.Where(expr.Has[models.User, models.User](r, ps...))))
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "relation model types do not match bound metadata") {
			t.Fatalf("promoted relation target accepted: %v", err)
		}
	}
	// Embedding does not itself invalidate a relation when its own plan agrees.
	r.Relation = models.UserRelations.Introducer
	if _, err := rowset.CompileRead(rowset.Read(q.Where(expr.Has[models.User, models.User](r)))); err != nil {
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
				"model child":          {orm.NumberGt(models.Users.Age, 0)},
			} {
				for operation, predicate := range map[string]orm.Predicate[models.User]{
					"Has":        orm.Has[models.User, models.User](relation, ps...),
					"DoesntHave": orm.Not(orm.Has(relation, ps...)),
				} {
					_, err := rowset.CompileRead(rowset.Read(q.Where(expr.ModelPredicate(predicate))))
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
