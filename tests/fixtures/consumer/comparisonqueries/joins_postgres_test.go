package comparisonqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type youngerAlias struct{}
type olderAlias struct{}
type extraAlias struct{}

func TestPostgresComputedJoinConditions(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		q := models.QueryUsers()
		younger, older := query.As[youngerAlias](q, "younger"), query.As[olderAlias](q, "older")
		y, o := models.UserFieldsAt(younger.Scope()), models.UserFieldsAt(older.Scope())
		adjacent := query.OnEqual(query.Add(y.Age, y.Age.Param(1)), o.Age)
		for _, test := range []struct {
			on    query.JoinOn[query.Alias[youngerAlias, models.User], query.Alias[olderAlias, models.User]]
			count int64
		}{
			{adjacent, 3}, {query.OnLess(y.Age, o.Age), 6}, {query.OnLessOrEqual(y.Age, o.Age), 10},
			{query.OnGreater(y.Age, o.Age), 6}, {query.OnGreaterOrEqual(y.Age, o.Age), 10},
			{query.OnNotEqual(y.Age, o.Age), 12}, {query.OnLike(query.Lower(y.Email), o.Email), 4},
			{query.OnEqual(y.Nickname, o.Nickname), 0}, {query.OnNotDistinctFrom(y.Nickname, o.Nickname), 16},
			{query.OnDistinctFrom(y.Nickname, o.Nickname), 0},
			{query.OnLessNullable(query.NullableRow(y.Age), query.NullableRow(o.Age)), 6},
		} {
			joined := query.InnerJoin(younger, older, test.on)
			left := models.UserFieldsAt(query.LeftScope(joined, younger.Scope()))
			count, err := query.SelectValue(joined, left.ID.Value()).Count(t.Context(), tx)
			if err != nil || count != test.count {
				t.Fatal(count, test.count, err)
			}
		}
		leftJoin := query.LeftJoin(younger, older, adjacent.WhereRight(o.Status.Eq(models.StatusActive)))
		l, r := models.UserFieldsAt(query.LeftScope(leftJoin, younger.Scope())), models.UserNullableFieldsAt(query.NullableRightScope(leftJoin, older.Scope()))
		pairs, err := reports.ProjectUserPair(leftJoin).SelectLeftID(query.Nullable(l.ID.Value())).SelectRightID(r.ID.Value()).Query().OrderBy(l.Age.Asc()).All(t.Context(), tx)
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
		extra := query.As[extraAlias](q.Where(models.UserFields().ID.Eq(users[0].ID)), "once")
		crossed := query.CrossJoin(leftJoin, extra)
		nullable := models.UserNullableFieldsAt(query.LeftNullableScope(crossed, query.NullableRightScope(leftJoin, older.Scope())))
		ids, err := query.SelectValue(crossed, nullable.ID.Value()).All(t.Context(), tx)
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
		rightJoin := query.RightJoin(younger, older, query.OnLess(y.Age, o.Age))
		right := models.UserFieldsAt(query.RightScope(rightJoin, older.Scope()))
		n, err := query.SelectValue(rightJoin, right.ID.Value()).Count(t.Context(), tx)
		if err != nil || n != 7 {
			t.Fatal("right range join", n, err)
		}
		full := query.FullJoin(younger, older, query.OnAnd(query.On(y.Level, o.Level), query.OnLess(y.Age, o.Age)))
		fullLeft := models.UserNullableFieldsAt(query.NullableLeftScope(full, younger.Scope()))
		n, err = query.SelectValue(full, fullLeft.ID.Value()).Count(t.Context(), tx)
		if err != nil || n != 8 {
			t.Fatal("full range join", n, err)
		}
		// PostgreSQL requires a hash/merge-capable FULL JOIN key. Keep its
		// unsupported pure-inequality behavior explicit rather than changing SQL.
		err = tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			unsupported := query.FullJoin(younger, older, query.OnLess(y.Age, o.Age))
			field := models.UserNullableFieldsAt(query.NullableLeftScope(unsupported, younger.Scope()))
			_, err := query.SelectValue(unsupported, field.ID.Value()).All(t.Context(), inner)
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
		q := models.QueryUsers()
		f := models.UserFields()
		left := query.As[youngerAlias](q.OrderBy(f.Age.Asc()).Limit(2), "younger")
		right := query.As[olderAlias](q.Where(f.Age.Gt(20)).OrderBy(f.Age.Asc()).Limit(1), "older")
		crossed := query.CrossJoin(left, right)
		l, r := models.UserFieldsAt(query.LeftScope(crossed, left.Scope())), models.UserFieldsAt(query.RightScope(crossed, right.Scope()))
		records := query.SelectRecord(crossed, query.LeftScope(crossed, left.Scope()))
		rows, err := records.OrderBy(l.Age.Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 2 || rows[0].Age != 20 || rows[1].Age != 21 {
			t.Fatal(rows, err)
		}
		filtered, err := records.Where(query.Equal(query.Add(l.Age, l.Age.Param(1)), r.Age)).All(t.Context(), tx)
		if err != nil || len(filtered) != 1 || filtered[0].Age != 20 {
			t.Fatal(filtered, err)
		}
		if locked, err := records.ForUpdate().All(t.Context(), tx); err != nil || len(locked) != 2 {
			t.Fatal(locked, err)
		}
		empty := query.CrossJoin(left, query.As[olderAlias](q.Limit(0), "empty"))
		emptyLeft := models.UserFieldsAt(query.LeftScope(empty, left.Scope()))
		n, err := query.SelectValue(empty, emptyLeft.ID.Value()).Count(t.Context(), tx)
		if err != nil || n != 0 {
			t.Fatal("empty Cartesian side", n, err)
		}
		return nil
	})
}
