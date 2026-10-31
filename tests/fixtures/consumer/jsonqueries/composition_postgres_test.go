package jsonqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/jsonqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type filteredDocuments struct{}
type previousDocument struct{}

func TestPostgresJSONSourceAndRelationScopes(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
		derived := rows.AsRecord[filteredDocuments](rows.CTE("dark_documents", rows.ModelRecord(expr.Where(q, expr.Eq(expr.JSONContains(expr.Value(orm.JSONExpr(f.Settings)), expr.Param(orm.JSONExpr(f.Settings), filter)), true)))), "selected")
		d := expr.At(rows.Scope(derived), orm.JSONExpr(f.Settings))
		theme := expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(d), jsonqueries.Theme)), codec.String[string]())
		if themes, err := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(derived, expr.AsSelected(theme)), expr.NullableEq(theme, "dark"))).All(t.Context(), tx); err != nil || len(themes) != 2 || themes[0] != value.Of("dark") {
			t.Fatal(themes, err)
		}
		if kinds, err := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(derived, expr.AsSelected(expr.JSONType(d))), expr.Eq(expr.JSONContainedByNullable(expr.NullableRow(d), expr.ParamOf(expr.NullableRow(d), value.Of(filter))), value.Of(true)))).All(t.Context(), tx); err != nil || len(kinds) != 2 {
			t.Fatal(kinds, err)
		}
		previous := rows.As[previousDocument](q, "previous")
		link := rows.Correlate(rows.From(q), previous)
		parent := rows.OuterScope(link, rows.ModelScope(q))
		child := rows.InnerScope(link, rows.Scope(previous))
		pathSelected := rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.At(child, orm.JSONExpr(f.Settings))), jsonqueries.Theme)), codec.String[string]()))).Where(expr.Less(expr.At(child, orm.KeyExpr(f.Key())), expr.At(parent, orm.KeyExpr(f.Key())))).OrderBy(expr.Desc(expr.At(child, orm.KeyExpr(f.Key())))).Limit(1)
		pathValues, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.OrderBy(orm.KeyAsc(f.Key()))), expr.AsSelected(rows.CorrelatedScalarNullableRowQuery(pathSelected)))).All(t.Context(), tx)
		if err != nil || len(pathValues) != 3 || !pathValues[0].IsNull() || pathValues[1] != value.Of("dark") || !pathValues[2].IsNull() {
			t.Fatal(pathValues, err)
		}
		selected := rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.JSONContains(expr.At(parent, orm.JSONExpr(f.Settings)), expr.At(child, orm.JSONExpr(f.Settings))))).Where(expr.Less(expr.At(child, orm.KeyExpr(f.Key())), expr.At(parent, orm.KeyExpr(f.Key())))).OrderBy(expr.Desc(expr.At(child, orm.KeyExpr(f.Key())))).Limit(1)
		values, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.OrderBy(orm.KeyAsc(f.Key()))), expr.AsSelected(rows.CorrelatedScalarRowQuery(selected)))).All(t.Context(), tx)
		if err != nil || len(values) != 3 || !values[0].IsNull() || values[1] != value.Of(false) || values[2] != value.Of(true) {
			t.Fatal(values, err)
		}
		policy := jsonqueries.DocumentLinks.Policy
		key := jsonqueries.Policies.Key()
		policy = policy.Where(expr.RelationFilter(policy, expr.Eq(expr.JSONContains(expr.Value(orm.KeyExpr(key)), expr.Param(orm.KeyExpr(key), original[0].PolicyID)), true)))
		if rows, err := orm.All(t.Context(), tx, jsonqueries.Documents.Query().Where(orm.Has(policy)).With(policy)); err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		return nil
	})
}

func TestJSONFailuresBeforeExecution(t *testing.T) {
	q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
	for _, predicate := range []expr.Predicate[jsonqueries.Document]{
		expr.Eq(expr.JSONContains(expr.Value(orm.JSONExpr(f.Settings)), expr.Param(orm.JSONExpr(f.Settings), value.JSON[jsonqueries.Preferences]{})), true),
		expr.Eq(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(f.Backup)), expr.NullableRow(expr.Param(orm.JSONExpr(f.Settings), value.JSON[jsonqueries.Preferences]{}))), value.Of(true)),
		expr.Eq(expr.JSONType(expr.Value(orm.JSONExpr(f.Settings))), database.JSONKind("typo")),
	} {
		if _, err := orm.All(t.Context(), queryfixture.NoQueries(t), expr.Where(q.Limit(0), predicate)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
