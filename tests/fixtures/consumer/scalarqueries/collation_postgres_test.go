package scalarqueries_test

import (
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresCollationOrdering(t *testing.T) {
	// runCalculations owns the env-gated testkit/postgres pool and isolated schema.
	runCalculations(t, func(tx *database.Tx) error {
		f := scalarqueries.Samples
		for i, name := range []scalarqueries.Label{"adam", "Zack", "bob"} {
			if _, err := tx.Exec(t.Context(), `UPDATE calculation_samples SET name = $1 WHERE id = $2`, string(name), i+1); err != nil {
				return err
			}
		}
		if _, err := orm.Insert(t.Context(), tx, f.Table, orm.NumberKeySet(f.ID, 4), orm.NumberSet(f.Quantity, 0), orm.NumberSet(f.Amount, decimal.FromInt64(0)), orm.NumberSet(f.Score, 0), orm.TextSet(f.Name, "Émile")); err != nil {
			return err
		}
		name := expr.Value(orm.TextExpr(f.Name))
		for _, tc := range []struct {
			collation expr.Collation
			want      []scalarqueries.Label
		}{
			{expr.CollationUnicode, []scalarqueries.Label{"adam", "bob", "Émile", "Zack"}},
			{expr.CollationC, []scalarqueries.Label{"Zack", "adam", "bob", "Émile"}},
		} {
			collated := expr.Collate(name, tc.collation)
			q := expr.OrderBy(f.Query(), expr.Asc(collated))
			got, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(collated))).All(t.Context(), tx)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatal("collated ordering and unchanged values", tc.collation, got, err)
			}
		}
		// Collation also retains the stored NULL/present distinction and named type.
		scalarValues(t, tx, expr.CollateNullableValue(expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(f.Note)))), expr.CollationUnicode), []value.Nullable[scalarqueries.Label]{value.Null[scalarqueries.Label](), value.Of(scalarqueries.Label("Écho")), value.Of(scalarqueries.Label("")), value.Null[scalarqueries.Label]()})
		counter := &queryfixture.QueryCounter{Executor: tx}
		for _, invalid := range []expr.Collation{"", "bad\x00name", expr.ICUCollation("en_US")} {
			q := expr.OrderBy(f.Query(), expr.Asc(expr.Collate(name, invalid)))
			if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.Row(name)))).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || counter.Queries.Load() != 0 {
				t.Fatal("invalid collation reached the database", err, counter.Queries.Load())
			}
		}
		return nil
	})
}
