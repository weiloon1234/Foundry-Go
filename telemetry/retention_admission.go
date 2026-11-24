package telemetry

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var (
	runtimeVersion     = expr.JSONFieldOf(func(p *store.Envelope[Runtime]) *int { return &p.Version })
	runtimeData        = expr.JSONFieldOf(func(p *store.Envelope[Runtime]) *Runtime { return &p.Data })
	runtimeHealth      = expr.JSONFieldOf(func(p *Runtime) *Health { return &p.Health })
	runtimeHealthState = expr.JSONFieldOf(func(p *Health) *foundation.State { return &p.State })
)

// reclaimEmptyRuns runs under the controls-row lock. Normal admission requires
// the entire deficit; an oversized deficit permits bounded cleanup-only progress.
func (s *storage) reclaimEmptyRuns(ctx context.Context, tx *database.Tx, now temporal.DateTime, needed int64) (int64, error) {
	if needed < 1 {
		return 0, nil
	}
	query, err := s.emptyRunReclaimQuery(now, int(min(needed, maxPruneRuns)))
	if err != nil {
		return 0, err
	}
	keys, err := rows.Read(rows.SelectValue(query, runs.Key())).All(ctx, tx)
	if err != nil || len(keys) == 0 || (needed <= maxPruneRuns && int64(len(keys)) != needed) {
		return 0, err
	}
	var result PruneResult
	if err := s.deleteRunKeys(ctx, tx, keys, &result); err != nil {
		return 0, err
	}
	return result.Runs, nil
}

func (s *storage) emptyRunReclaimQuery(now temporal.DateTime, limit int) (orm.Query[runRow], error) {
	quietBefore, err := now.Add(-s.config.Retention.emptyRunQuietPeriod())
	if err != nil {
		return orm.Query[runRow]{}, err
	}
	query := runs.Query().Where(orm.Lt(runs.HeartbeatAt, quietBefore)).
		OrderBy(orm.Asc(runs.HeartbeatAt), orm.Asc(runs.Key())).Limit(limit)
	root := expr.JSONRoot(expr.At(rows.ModelScope(query), runs.Runtime))
	state := expr.JSONProperty(expr.JSONProperty(expr.JSONProperty(root, runtimeData), runtimeHealth), runtimeHealthState)
	// JSON equality avoids casting malformed legacy fields. Missing, null,
	// wrong-shaped and unknown states never satisfy these predicates.
	version, err := value.NewJSON(1)
	if err != nil {
		return orm.Query[runRow]{}, err
	}
	stopping, err := value.NewJSON(foundation.Stopping)
	if err != nil {
		return orm.Query[runRow]{}, err
	}
	query = expr.Where(query, expr.And(
		expr.NullableEq(expr.JSONProperty(root, runtimeVersion).JSON(), version),
		expr.NullableEq(state.JSON(), stopping),
	))
	query = withoutLiveRunReceipts(query, now)
	return emptyRunData(query), nil
}

func emptyRunData(query orm.Query[runRow]) orm.Query[runRow] {
	metric := rows.Correlate(rows.From(query), rows.From(metrics.Query()))
	metric = metric.Where(expr.Equal(
		expr.At(rows.InnerScope(metric, rows.ModelScope(metrics.Query())), metrics.RunID),
		expr.At(rows.OuterScope(metric, rows.ModelScope(query)), runs.Key()),
	))
	alert := rows.Correlate(rows.From(query), rows.From(alerts.Query()))
	alert = alert.Where(expr.Equal(
		expr.At(rows.InnerScope(alert, rows.ModelScope(alerts.Query())), alerts.RunID),
		expr.At(rows.OuterScope(alert, rows.ModelScope(query)), runs.Key()),
	))
	return expr.Where(query, expr.And(expr.Not(metric.Exists()), expr.Not(alert.Exists())))
}
