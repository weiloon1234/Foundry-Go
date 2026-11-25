package keyqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin computed-query-keys
func Buckets() rows.Query[models.User, BucketCount] {
	u := models.Users
	bucket := expr.When(expr.ModelPredicate(orm.Gt(u.Age, 20)), expr.Param(u.Age, 1)).Else(expr.Param(u.Age, 0))
	return rows.OrderBy(rows.GroupBy(rows.Select(rows.From(models.Users.Query()), BucketCounts.Row,
		rows.MapSelected(BucketCounts.Bucket, expr.AsSelected(bucket)),
		rows.MapSelected(BucketCounts.Count, expr.AsSelected(expr.Count[models.User]())),
	), expr.Group(bucket)), expr.Asc(bucket))
}

// doc-example:end computed-query-keys
