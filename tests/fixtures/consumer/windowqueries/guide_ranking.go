package windowqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin model-windows
func Ranking() rows.Query[models.Order, RankingRow] {
	base := models.Orders.Query()
	o := models.Orders
	peers := expr.WindowFor(rows.ModelScope(base)).PartitionBy(expr.Group(expr.Value(o.BuyerID))).OrderBy(expr.Asc(expr.Value(o.TotalCents)))
	ordered := peers.OrderBy(expr.Asc(expr.Value(o.ID)))
	return rows.OrderBy(rows.Select(rows.From(base), Rankings, rows.MapSelected(Rankings.ID, expr.AsSelected(expr.Row(expr.Value(o.ID)))), rows.MapSelected(Rankings.BuyerID, expr.AsSelected(expr.Row(expr.Value(o.BuyerID)))), rows.MapSelected(Rankings.Number, expr.RowNumber(ordered)), rows.MapSelected(Rankings.Rank, expr.Rank(peers)), rows.MapSelected(Rankings.Dense, expr.DenseRank(peers)), rows.MapSelected(Rankings.Percent, expr.PercentRank(peers)), rows.MapSelected(Rankings.Cumulative, expr.CumeDist(peers)), rows.MapSelected(Rankings.Bucket, expr.NTile(2, ordered))), expr.Asc(expr.Value(o.BuyerID)), expr.Asc(expr.Value(o.TotalCents)), expr.Asc(expr.Value(o.ID)))
}

// doc-example:end model-windows
