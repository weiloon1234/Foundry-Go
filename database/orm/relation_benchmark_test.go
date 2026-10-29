package orm

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// Complete public read/compile/hydrate/attach path: 1000 parents, one child and
// grandchild per parent, batches of 500. Driver work and five statements are timed.
func BenchmarkRelationTree1000Depth2(b *testing.B) {
	p := people()
	r := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	o := &relationOracle{}
	for i := int64(1); i <= 1000; i++ {
		o.rows = append(o.rows, relationRow(i, nil, "parent"), relationRow(1000+i, i, "child"), relationRow(2000+i, 1000+i, "grandchild"))
	}
	ctx := context.Background()
	db, err := database.Open(ctx, database.Adapter{Connector: o.connector()}, database.DefaultPoolConfig())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := db.Close(ctx); err != nil {
			b.Error(err)
		}
	})
	q := p.Query().Where(TextEq(p.Name, "parent")).With(r.With(r))
	if _, err := All(ctx, db, q); err != nil {
		b.Fatal(err)
	}
	o.calls, o.closed = 0, 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parents, err := All(ctx, db, q)
		if err != nil || len(parents) != 1000 {
			b.Fatalf("rows=%d: %v", len(parents), err)
		}
		for i, parent := range parents {
			if parent.ID != int64(i+1) || parent.Reports.Len() != 1 {
				b.Fatal("wrong parent/child")
			}
			for _, child := range parent.Reports.All() {
				if child.ID != 1000+parent.ID || child.Reports.Len() != 1 {
					b.Fatal("wrong child/grandchild")
				}
				for _, grand := range child.Reports.All() {
					if grand.ID != 2000+parent.ID {
						b.Fatal("wrong grandchild")
					}
				}
			}
		}
	}
	b.StopTimer()
	if o.calls != 5*b.N || o.closed != o.calls {
		b.Fatalf("statements=%d closed=%d want=%d", o.calls, o.closed, 5*b.N)
	}
	b.ReportMetric(float64(o.calls)/float64(b.N), "statements/op")
	b.ReportMetric(1000, "parents/op")
}
