package goway_test

import (
	"fmt"
	n "foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

func TestAdjustmentsGolden(t *testing.T) {
	for _, subtract := range []bool{false, true} {
		t.Run(fmt.Sprint(subtract), func(t *testing.T) {
			var transcripts []string
			{
				g := &goldenCapture{}
				db := g.open(t.Context())
				defer db.Close(t.Context())

				f := n.Orders
				a := f.TotalCents.Increment(5, orm.AtMost(int64(100)))
				if subtract {
					a = f.TotalCents.Decrement(5, orm.AtLeast(int64(0)))
				}
				_, err := f.Query().UpdateAll(t.Context(), db, a)
				check(err)
				_, err = n.LedgerEntries.Query().UpdateAll(t.Context(), db, n.LedgerEntries.Balance.Decrement(must(decimal.Parse("1.25"))))
				check(err)

				transcripts = append(transcripts, g.output.String())
			}
			if !oracleEqual(t, "adjustments_golden/1", transcripts[0]) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}
