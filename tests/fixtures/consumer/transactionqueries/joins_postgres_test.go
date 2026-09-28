package transactionqueries

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestPostgresTransactionOuterAndCrossJoins(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	left := query.AsTransaction[claimedAlias](QueryRecords().Where(RecordFields().ID.Lte(2)).ForKeyShare(), "left_records")
	right := query.AsTransaction[otherAlias](QueryRecords().Where(RecordFields().ID.Gte(2)).ForKeyShare(), "right_records")
	on := query.On(RecordFieldsAt(left.Scope()).ID, RecordFieldsAt(right.Scope()).ID)
	for _, kind := range []string{"left", "right", "full", "cross"} {
		t.Run(kind, func(t *testing.T) {
			if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				var pairs []Pair
				var err error
				switch kind {
				case "left":
					joined := query.TransactionLeftJoin(left, right, on)
					a := RecordFieldsAt(query.LeftScope(joined, left.Scope()))
					b := RecordNullableFieldsAt(query.NullableRightScope(joined, right.Scope()))
					pairs, err = ProjectTransactionPair(joined).SelectLeft(query.NullableRow(a.ID).Value()).SelectRight(b.ID.Value()).Query().All(ctx, tx)
				case "right":
					joined := query.TransactionRightJoin(left, right, on)
					a := RecordNullableFieldsAt(query.NullableLeftScope(joined, left.Scope()))
					b := RecordFieldsAt(query.RightScope(joined, right.Scope()))
					pairs, err = ProjectTransactionPair(joined).SelectLeft(a.ID.Value()).SelectRight(query.NullableRow(b.ID).Value()).Query().All(ctx, tx)
				case "full":
					joined := query.TransactionFullJoin(left, right, on)
					a := RecordNullableFieldsAt(query.NullableLeftScope(joined, left.Scope()))
					b := RecordNullableFieldsAt(query.NullableRightScope(joined, right.Scope()))
					pairs, err = ProjectTransactionPair(joined).SelectLeft(a.ID.Value()).SelectRight(b.ID.Value()).Query().All(ctx, tx)
				case "cross":
					joined := query.TransactionCrossJoin(left, right)
					a := RecordFieldsAt(query.LeftScope(joined, left.Scope()))
					b := RecordFieldsAt(query.RightScope(joined, right.Scope()))
					pairs, err = ProjectTransactionPair(joined).SelectLeft(query.NullableRow(a.ID).Value()).SelectRight(query.NullableRow(b.ID).Value()).Query().All(ctx, tx)
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
