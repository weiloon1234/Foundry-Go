package intervalqueries_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestPostgresIntervalArithmeticFailure(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, _ []intervalqueries.Sample) error {
		q, f := intervalqueries.QueryIntervalSamples(), intervalqueries.SampleFields()
		err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := query.SelectValue(q, query.NegateInterval(f.Period.Param(temporal.Months(math.MinInt32))).Value()).All(t.Context(), inner)
			return err
		})
		var pgError *database.Error
		if !errors.As(err, &pgError) || pgError.SQLState() != "22008" {
			t.Fatal("calendar overflow was not an interval error", err)
		}
		longest := interval(t, 0, 0, (time.Duration(math.MaxInt64)/time.Microsecond)*time.Microsecond)
		if rows, err := query.SelectValue(q, query.AddIntervals(f.Period.Param(longest), f.Period.Param(longest)).Value()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("elapsed overflow silently normalized into days", rows, err)
		}
		// Arithmetic decode failures do not abort the surrounding SQL transaction.
		if count, err := q.Count(t.Context(), tx); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		return nil
	})
}
