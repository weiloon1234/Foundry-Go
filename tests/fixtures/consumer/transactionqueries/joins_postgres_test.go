package transactionqueries

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestPostgresTransactionOuterAndCrossJoins(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	left := rows.AsTransaction[claimedAlias](rows.LockedRecord(orm.ForKeyShare(Records.Query().Where(orm.NumberKeyLte(orm.NumberKey(Records.ID), 2)))), "left_records")
	right := rows.AsTransaction[otherAlias](rows.LockedRecord(orm.ForKeyShare(Records.Query().Where(orm.NumberKeyGte(orm.NumberKey(Records.ID), 2)))), "right_records")
	on := rows.OnEqual(recordAt(rows.TransactionScope(left)).ID, recordAt(rows.TransactionScope(right)).ID)
	for _, kind := range []string{"left", "right", "full", "cross"} {
		t.Run(kind, func(t *testing.T) {
			if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				var pairs []Pair
				var err error
				switch kind {
				case "left":
					joined := rows.TransactionLeftJoin(left, right, on)
					a := recordAt(rows.LeftScope(joined, rows.TransactionScope(left)))
					b := nullableRecordAt(rows.NullableRightScope(joined, rows.TransactionScope(right)))
					pairs, err = rows.SelectTransaction(joined, Pairs, rows.MapSelected(Pairs.Left, expr.NullableRow(a.ID).Value()), rows.MapSelected(Pairs.Right, expr.Row(b.ID).Value())).All(ctx, tx)
				case "right":
					joined := rows.TransactionRightJoin(left, right, on)
					a := nullableRecordAt(rows.NullableLeftScope(joined, rows.TransactionScope(left)))
					b := recordAt(rows.RightScope(joined, rows.TransactionScope(right)))
					pairs, err = rows.SelectTransaction(joined, Pairs, rows.MapSelected(Pairs.Left, expr.Row(a.ID).Value()), rows.MapSelected(Pairs.Right, expr.NullableRow(b.ID).Value())).All(ctx, tx)
				case "full":
					joined := rows.TransactionFullJoin(left, right, on)
					a := nullableRecordAt(rows.NullableLeftScope(joined, rows.TransactionScope(left)))
					b := nullableRecordAt(rows.NullableRightScope(joined, rows.TransactionScope(right)))
					pairs, err = rows.SelectTransaction(joined, Pairs, rows.MapSelected(Pairs.Left, expr.Row(a.ID).Value()), rows.MapSelected(Pairs.Right, expr.Row(b.ID).Value())).All(ctx, tx)
				case "cross":
					joined := rows.TransactionCrossJoin(left, right)
					a := recordAt(rows.LeftScope(joined, rows.TransactionScope(left)))
					b := recordAt(rows.RightScope(joined, rows.TransactionScope(right)))
					pairs, err = rows.SelectTransaction(joined, Pairs, rows.MapSelected(Pairs.Left, expr.NullableRow(a.ID).Value()), rows.MapSelected(Pairs.Right, expr.NullableRow(b.ID).Value())).All(ctx, tx)
				}
				if err != nil {
					return err
				}
				want := map[string][][2]int64{
					"left":  {{1, 0}, {2, 2}},
					"right": {{2, 2}, {0, 3}},
					"full":  {{1, 0}, {2, 2}, {0, 3}},
					"cross": {{1, 2}, {1, 3}, {2, 2}, {2, 3}},
				}[kind]
				seen := make(map[[2]int64]bool)
				for _, pair := range pairs {
					a, hasA := pair.Left.Get()
					b, hasB := pair.Right.Get()
					if hasA != (a != 0) || hasB != (b != 0) {
						return errors.New("outer join lost SQL nullability")
					}
					seen[[2]int64{a, b}] = true
				}
				if len(pairs) != len(want) {
					return errors.New("transaction join changed row multiplicity")
				}
				for _, pair := range want {
					if !seen[pair] {
						return errors.New("transaction join lost a matched or unmatched pair")
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
