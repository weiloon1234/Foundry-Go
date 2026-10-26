package conflictadvanced

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type lookup struct{}

func Write(ctx context.Context, db database.Transactor) (value.Optional[m.WriteRecord], error) {
	f := m.WriteRecords
	records := r.ConflictRowsOf(f.Query())
	stored, proposed := expr.At(records.Stored(), f.Amount), expr.At(records.Proposed(), f.Amount)
	policy := r.OnConflictKeys(expr.Group(expr.Add(expr.Value(f.Amount), expr.Value(f.Amount)))).DoUpdate(r.SetConflictValue(f.Amount, expr.Add(stored, proposed)))
	policy = r.ConflictTargetWhere(policy, expr.Value(f.Enabled).Eq(true))
	alias := r.As[lookup](f.Query(), "lookup")
	c := r.Correlate(records, alias)
	inner := expr.At(r.InnerScope(c, r.Scope(alias)), f.Note)
	scalar := r.SelectCorrelatedScalar(c, expr.Row(inner).Value()).Limit(1)
	policy = policy.DoUpdate(r.SetConflictValue(f.Note, r.CorrelatedScalarNullableRowQuery(scalar)))
	policy = r.ConflictWhereRows(policy, expr.Greater(proposed, stored), scalar.Exists())
	return orm.Upsert(ctx, db, f.Query(), policy, f.Name.Set("input"))
}
