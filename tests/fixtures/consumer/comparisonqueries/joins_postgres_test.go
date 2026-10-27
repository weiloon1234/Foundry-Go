package comparisonqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type youngerAlias struct{}
type olderAlias struct{}
type extraAlias struct{}

func TestPostgresComputedJoinConditions(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		q := models.Users.Query()
		younger, older := rowset.As[youngerAlias](q, "younger"), rowset.As[olderAlias](q, "older")
		y, o := rowset.Scope(younger), rowset.Scope(older)
		adjacent := rowset.OnEqual(expr.Add(expr.At(y, models.Users.Age), expr.Row(expr.At(y, models.Users.Age)).Value().Param(1)), expr.At(o, models.Users.Age))
		for _, test := range []struct {
			on    rowset.JoinOn[rowset.Alias[youngerAlias, models.User], rowset.Alias[olderAlias, models.User]]
			count int64
		}{
			{adjacent, 3}, {rowset.OnLess(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)), 6}, {rowset.OnLessOrEqual(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)), 10},
			{rowset.OnGreater(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)), 6}, {rowset.OnGreaterOrEqual(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)), 10},
			{rowset.OnNotEqual(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)), 12}, {rowset.OnLike(expr.Lower(expr.At(y, models.Users.Email)), expr.At(o, models.Users.Email)), 4},
			{rowset.OnEqual(expr.At(y, models.Users.Nickname), expr.At(o, models.Users.Nickname)), 0}, {rowset.OnNotDistinctFrom(expr.At(y, models.Users.Nickname), expr.At(o, models.Users.Nickname)), 16},
			{rowset.OnDistinctFrom(expr.At(y, models.Users.Nickname), expr.At(o, models.Users.Nickname)), 0},
			{rowset.OnLessNullable(expr.NullableRow(expr.At(y, models.Users.Age)), expr.NullableRow(expr.At(o, models.Users.Age))), 6},
		} {
			joined := rowset.InnerJoin(younger, older, test.on)
			left := rowset.LeftScope(joined, rowset.Scope(younger))
			count, err := rowset.ReadScalar(rowset.SelectScalar(joined, expr.Row(expr.At(left, models.Users.ID)).Value())).Count(t.Context(), tx)
			if err != nil || count != test.count {
				t.Fatal(count, test.count, err)
			}
		}
		leftJoin := rowset.LeftJoin(younger, older, adjacent.WhereRight(expr.At(o, models.Users.Status).Eq(models.StatusActive)))
		l, r := rowset.LeftScope(leftJoin, rowset.Scope(younger)), rowset.NullableRightScope(leftJoin, rowset.Scope(older))
		pairs, err := rowset.Read(rowset.OrderBy(rowset.Select(leftJoin, reports.UserPairs.Row,
			rowset.MapSelected(reports.UserPairs.LeftID, expr.Nullable(expr.Row(expr.At(l, models.Users.ID)).Value())),
			rowset.Map(reports.UserPairs.RightID, expr.AtNullable(r, models.Users.ID)),
		), expr.At(l, models.Users.Age).Asc())).All(t.Context(), tx)
		if err != nil || len(pairs) != 4 {
			t.Fatal(pairs, err)
		}
		if !pairs[0].RightID.IsNull() || !pairs[3].RightID.IsNull() {
			t.Fatal("outer ON conditions removed unmatched rows", pairs)
		}
		if id, present := pairs[1].RightID.Get(); !present || id != users[2].ID {
			t.Fatal(pairs)
		}
		// Preserve an already-nullable side when appending an independent Cartesian input.
		extra := rowset.As[extraAlias](q.Where(models.Users.ID.Eq(users[0].ID)), "once")
		crossed := rowset.CrossJoin(leftJoin, extra)
		nullable := rowset.LeftNullableScope(crossed, rowset.NullableRightScope(leftJoin, rowset.Scope(older)))
		ids, err := rowset.ReadScalar(rowset.SelectScalar(crossed, expr.Row(expr.AtNullable(nullable, models.Users.ID)).Value())).All(t.Context(), tx)
		if err != nil || len(ids) != 4 {
			t.Fatal(ids, err)
		}
		missing := 0
		for _, id := range ids {
			if id.IsNull() {
				missing++
			}
		}
		if missing != 2 {
			t.Fatal("cross join erased existing nullability", ids)
		}
		// Range conditions retain right/full outer-join cardinality.
		rightJoin := rowset.RightJoin(younger, older, rowset.OnLess(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)))
		right := rowset.RightScope(rightJoin, rowset.Scope(older))
		n, err := rowset.ReadScalar(rowset.SelectScalar(rightJoin, expr.Row(expr.At(right, models.Users.ID)).Value())).Count(t.Context(), tx)
		if err != nil || n != 7 {
			t.Fatal("right range join", n, err)
		}
		full := rowset.FullJoin(younger, older, rowset.OnAnd(rowset.On(expr.At(y, models.Users.Level), expr.At(o, models.Users.Level)), rowset.OnLess(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age))))
		fullLeft := rowset.NullableLeftScope(full, rowset.Scope(younger))
		n, err = rowset.ReadScalar(rowset.SelectScalar(full, expr.Row(expr.AtNullable(fullLeft, models.Users.ID)).Value())).Count(t.Context(), tx)
		if err != nil || n != 8 {
			t.Fatal("full range join", n, err)
		}
		// PostgreSQL requires a hash/merge-capable FULL JOIN key. Keep its
		// unsupported pure-inequality behavior explicit rather than changing SQL.
		err = tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			unsupported := rowset.FullJoin(younger, older, rowset.OnLess(expr.At(y, models.Users.Age), expr.At(o, models.Users.Age)))
			field := rowset.NullableLeftScope(unsupported, rowset.Scope(younger))
			_, err := rowset.ReadScalar(rowset.SelectScalar(unsupported, expr.Row(expr.AtNullable(field, models.Users.ID)).Value())).All(t.Context(), inner)
			return err
		})
		var dbError *database.Error
		if !errors.As(err, &dbError) || dbError.SQLState() != "0A000" {
			t.Fatal("unsupported full comparison join lost database feature error", err)
		}
		return nil
	})
}

func TestPostgresCartesianSourceBoundaries(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, _ []models.User) error {
		q := models.Users.Query()
		f := models.Users
		left := rowset.As[youngerAlias](q.OrderBy(f.Age.Asc()).Limit(2), "younger")
		right := rowset.As[olderAlias](q.Where(f.Age.Gt(20)).OrderBy(f.Age.Asc()).Limit(1), "older")
		crossed := rowset.CrossJoin(left, right)
		l, r := rowset.LeftScope(crossed, rowset.Scope(left)), rowset.RightScope(crossed, rowset.Scope(right))
		records := rowset.SelectRecord(crossed, rowset.LeftScope(crossed, rowset.Scope(left)))
		rows, err := rowset.ReadRecord(records.OrderBy(expr.At(l, models.Users.Age).Asc())).All(t.Context(), tx)
		if err != nil || len(rows) != 2 || rows[0].Age != 20 || rows[1].Age != 21 {
			t.Fatal(rows, err)
		}
		filtered, err := rowset.ReadRecord(records.Where(expr.Equal(expr.Add(expr.At(l, models.Users.Age), expr.Row(expr.At(l, models.Users.Age)).Value().Param(1)), expr.At(r, models.Users.Age)))).All(t.Context(), tx)
		if err != nil || len(filtered) != 1 || filtered[0].Age != 20 {
			t.Fatal(filtered, err)
		}
		if locked, err := rowset.ForUpdate(records).All(t.Context(), tx); err != nil || len(locked) != 2 {
			t.Fatal(locked, err)
		}
		empty := rowset.CrossJoin(left, rowset.As[olderAlias](q.Limit(0), "empty"))
		emptyLeft := rowset.LeftScope(empty, rowset.Scope(left))
		n, err := rowset.ReadScalar(rowset.SelectScalar(empty, expr.Row(expr.At(emptyLeft, models.Users.ID)).Value())).Count(t.Context(), tx)
		if err != nil || n != 0 {
			t.Fatal("empty Cartesian side", n, err)
		}
		return nil
	})
}
