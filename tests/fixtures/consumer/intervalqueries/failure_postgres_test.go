package intervalqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"math"
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestPostgresIntervalArithmeticFailure(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, _ []intervalqueries.Sample) error {
		q, f := intervalqueries.Samples.Query(), intervalqueries.Samples
		err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.NegateInterval(expr.Param(orm.IntervalExpr(f.Period), temporal.Months(math.MinInt32)))))).All(t.Context(), inner)
			return err
		})
		var pgError *database.Error
		if !errors.As(err, &pgError) || pgError.SQLState() != "22008" {
			t.Fatal("calendar overflow was not an interval error", err)
		}
		longest := interval(t, 0, 0, (time.Duration(math.MaxInt64)/time.Microsecond)*time.Microsecond)
		if rows, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.AddIntervals(expr.Param(orm.IntervalExpr(f.Period), longest), expr.Param(orm.IntervalExpr(f.Period), longest))))).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("elapsed overflow silently normalized into days", rows, err)
		}
		// Arithmetic decode failures do not abort the surrounding SQL transaction.
		if count, err := orm.Count(t.Context(), tx, q); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		return nil
	})
}
