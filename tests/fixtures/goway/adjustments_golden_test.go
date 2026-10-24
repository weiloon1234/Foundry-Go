package goway_test

import (
	"fmt"
	n "foundry.test/goway/models"
	o "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

func TestAdjustmentsGolden(t *testing.T) {
	for _, subtract := range []bool{false, true} {
		t.Run(fmt.Sprint(subtract), func(t *testing.T) {
			var transcripts []string
			for _, old := range []bool{true, false} {
				g := &goldenCapture{}
				db := g.open(t.Context())
				defer db.Close(t.Context())
				if old {
					f := o.OrderFields()
					q := o.QueryOrders()
					if subtract {
						_, err := q.Where(query.GreaterOrEqual(query.Subtract(f.TotalCents, query.Param(f.TotalCents, int64(5))), query.Param(f.TotalCents, int64(0)))).Decrement(t.Context(), db, f.TotalCents.By(5))
						check(err)
					} else {
						_, err := q.Where(query.LessOrEqual(query.Add(f.TotalCents, query.Param(f.TotalCents, int64(5))), query.Param(f.TotalCents, int64(100)))).Increment(t.Context(), db, f.TotalCents.By(5))
						check(err)
					}
					lf := o.LedgerEntryFields()
					_, err := o.QueryLedgerEntries().Decrement(t.Context(), db, lf.Balance.By(must(decimal.Parse("1.25"))))
					check(err)
				} else {
					f := n.Orders
					a := f.TotalCents.Increment(5, orm.AtMost(int64(100)))
					if subtract {
						a = f.TotalCents.Decrement(5, orm.AtLeast(int64(0)))
					}
					_, err := f.Query().UpdateAll(t.Context(), db, a)
					check(err)
					_, err = n.LedgerEntries.Query().UpdateAll(t.Context(), db, n.LedgerEntries.Balance.Decrement(must(decimal.Parse("1.25"))))
					check(err)
				}
				transcripts = append(transcripts, g.output.String())
			}
			if transcripts[0] != transcripts[1] {
				t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
			}
		})
	}
}
