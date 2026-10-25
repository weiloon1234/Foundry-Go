package windowqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestWindowReportSQLParity(t *testing.T) {
	got, err := rows.Compile(ranking())
	if err != nil {
		t.Fatal(err)
	}
	base := models.QueryOrders()
	o := models.OrderFields()
	peers := query.WindowFor(base).PartitionBy(o.BuyerID.Group()).OrderBy(o.TotalCents.Asc())
	ordered := peers.OrderBy(o.ID.Asc())
	want, err := windowqueries.SelectRankingRow(base, windowqueries.RankingRowSelection[models.Order]{ID: query.Value(o.ID), BuyerID: query.Value(o.BuyerID), Number: query.RowNumber(ordered), Rank: query.Rank(peers), Dense: query.DenseRank(peers), Percent: query.PercentRank(peers), Cumulative: query.CumeDist(peers), Bucket: query.NTile(2, ordered)}).OrderBy(o.BuyerID.Asc(), o.TotalCents.Asc(), o.ID.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("window report changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}
