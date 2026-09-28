package jsonqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type filteredDocuments struct{}
type previousDocument struct{}

func TestPostgresJSONSourceAndRelationScopes(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
		filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
		derived := query.As[filteredDocuments](query.CTE("dark_documents", q.Where(f.Settings.Contains(filter))), "selected")
		d := jsonqueries.DocumentFieldsAt(derived.Scope())
		if themes, err := query.SelectValue(derived, d.Settings.Properties().Theme.Scalar().Value()).Where(d.Settings.Properties().Theme.Scalar().Eq("dark")).All(t.Context(), tx); err != nil || len(themes) != 2 || themes[0] != value.Of("dark") {
			t.Fatal(themes, err)
		}
		if kinds, err := query.SelectValue(derived, d.Settings.Kind().Value()).Where(d.Settings.ContainedBy(filter)).All(t.Context(), tx); err != nil || len(kinds) != 2 {
			t.Fatal(kinds, err)
		}
		previous := query.As[previousDocument](q, "previous")
		link := query.Correlate(q, previous)
		parent := jsonqueries.DocumentFieldsAt(query.OuterScope(link, q.Scope()))
		child := jsonqueries.DocumentFieldsAt(query.InnerScope(link, previous.Scope()))
		pathSelected := query.SelectCorrelatedValue(link, child.Settings.Properties().Theme.Scalar().Value()).Where(child.ID.LtColumn(parent.ID)).OrderBy(child.ID.Desc()).Limit(1)
		pathValues, err := query.SelectValue(q.OrderBy(f.ID.Asc()), query.CorrelatedScalarNullableQuery(pathSelected)).All(t.Context(), tx)
		if err != nil || len(pathValues) != 3 || !pathValues[0].IsNull() || pathValues[1] != value.Of("dark") || !pathValues[2].IsNull() {
			t.Fatal(pathValues, err)
		}
		selected := query.SelectCorrelatedValue(link, query.JSONContains(parent.Settings, child.Settings).Value()).Where(child.ID.LtColumn(parent.ID)).OrderBy(child.ID.Desc()).Limit(1)
		values, err := query.SelectValue(q.OrderBy(f.ID.Asc()), query.CorrelatedScalarQuery(selected)).All(t.Context(), tx)
		if err != nil || len(values) != 3 || !values[0].IsNull() || values[1] != value.Of(false) || values[2] != value.Of(true) {
			t.Fatal(values, err)
		}
		policy := jsonqueries.DocumentRelations().Policy.Where(jsonqueries.PolicyFields().ID.Contains(original[0].PolicyID))
		if rows, err := q.WhereHas(policy).With(policy).All(t.Context(), tx); err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		return nil
	})
}

func TestJSONFailuresBeforeExecution(t *testing.T) {
	q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
	for _, predicate := range []query.Predicate[jsonqueries.Document]{
		f.Settings.Contains(value.JSON[jsonqueries.Preferences]{}),
		f.Backup.Contains(value.JSON[jsonqueries.Preferences]{}),
		f.Settings.Kind().Eq(query.JSONKind("typo")),
	} {
		if _, err := q.Limit(0).Where(predicate).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
