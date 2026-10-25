package keyqueries

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

type BucketCountColumns struct {
	rows.Row[BucketCount]
	Bucket rows.Field[BucketCount, int]
	Count  rows.Field[BucketCount, int64]
}

var BucketCounts = rows.Define(func(b rows.Builder[BucketCount], r *BucketCount) BucketCountColumns {
	return BucketCountColumns{Row: b.Row(), Bucket: rows.NumberOf(b, &r.Bucket), Count: rows.NumberOf(b, &r.Count)}
})
