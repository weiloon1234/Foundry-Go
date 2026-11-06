package telemetry

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// PruneResult counts confirmed telemetry deletions, including metric and receipt
// rows removed by run cascades. An uncertain transaction returns no counts; a
// committed transaction can return counts together with a completion error.
type PruneResult struct {
	Metrics, Issues, Events, Receipts, Runs int64
}

func (s *storage) prune(ctx context.Context) (PruneResult, error) {
	if err := s.enter(ctx); err != nil {
		return PruneResult{}, err
	}
	defer func() { <-s.gate }()
	attempt, cancel := context.WithTimeout(ctx, s.config.FlushTimeout)
	defer cancel()
	var result PruneResult
	tx, err := s.within(attempt, func(tx *database.Tx) error {
		now, err := serverTime(attempt, tx)
		if err != nil {
			return err
		}
		minute, err := now.Add(-s.config.Retention.Minute)
		if err != nil {
			return err
		}
		hour, err := now.Add(-s.config.Retention.Hour)
		if err != nil {
			return err
		}
		issue, err := now.Add(-s.config.Retention.Issue)
		if err != nil {
			return err
		}
		security, err := now.Add(-s.config.Retention.Security)
		if err != nil {
			return err
		}

		metricQuery := metrics.Query().Where(orm.Or(
			orm.And(orm.NumberEq(metrics.IntervalSeconds, 60), orm.TimeLt(metrics.BucketAt, minute)),
			orm.And(orm.NumberEq(metrics.IntervalSeconds, 3600), orm.TimeLt(metrics.BucketAt, hour)),
		)).OrderBy(orm.TimeAsc(metrics.BucketAt), orm.KeyAsc(metrics.Key())).Limit(maxPruneKeys)
		result.Metrics, err = pruneSelected(attempt, tx, metrics.Table, metricQuery)
		if err != nil {
			return err
		}
		issueQuery := issues.Query().Where(orm.Or(
			orm.And(orm.EnumEq(issues.Source, SourceSecurity), orm.TimeLt(issues.LastAt, security)),
			orm.And(orm.EnumNe(issues.Source, SourceSecurity), orm.TimeLt(issues.LastAt, issue)),
		)).OrderBy(orm.TimeAsc(issues.LastAt), orm.KeyAsc(issues.Key())).Limit(maxPruneKeys)
		result.Issues, err = pruneSelected(attempt, tx, issues.Table, issueQuery)
		if err != nil {
			return err
		}
		eventQuery := events.Query().Where(orm.Or(
			orm.And(orm.EnumEq(events.Source, SourceSecurity), orm.TimeLt(events.CreatedAt, security)),
			orm.And(orm.EnumNe(events.Source, SourceSecurity), orm.TimeLt(events.CreatedAt, issue)),
		)).OrderBy(orm.TimeAsc(events.CreatedAt), orm.KeyAsc(events.Key())).Limit(maxPruneKeys)
		result.Events, err = pruneSelected(attempt, tx, events.Table, eventQuery)
		if err != nil {
			return err
		}
		// RetainUntil belongs to the writer that admitted the batch. A pruner
		// configured with a shorter retry lifetime cannot shorten that horizon.
		receiptQuery := flushes.Query().Where(orm.TimeLte(flushes.RetainUntil, now)).
			OrderBy(orm.TimeAsc(flushes.RetainUntil), orm.KeyAsc(flushes.Key())).Limit(maxPruneKeys)
		result.Receipts, err = pruneSelected(attempt, tx, flushes.Table, receiptQuery)
		if err != nil {
			return err
		}
		return s.pruneRuns(attempt, tx, now, &result)
	})
	if err != nil && !storageInterrupted(ctx, err) {
		s.collector.mu.Lock()
		s.collector.failures = saturatingAdd(s.collector.failures, 1)
		s.collector.mu.Unlock()
	}
	if tx == nil || tx.State() != database.TxCommitted {
		if err == nil {
			err = database.NewError("telemetry prune", database.CommitUnknown)
		}
		return PruneResult{}, err
	}
	return result, err
}

// pruneSelected reads only bounded primary keys, then deletes exactly those keys
// in the same locked transaction. Selection ordering/limits never enter DELETE.
func pruneSelected[M any, K comparable](ctx context.Context, tx *database.Tx, table orm.Table[M, K], query orm.Query[M]) (int64, error) {
	keys, err := rows.Read(rows.SelectValue(query, orm.KeyExpr(table.Key()))).All(ctx, tx)
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	return orm.DeleteAll(ctx, tx, table.Query().Where(orm.KeyIn(table.Key(), keys...)))
}

func (s *storage) pruneRuns(ctx context.Context, tx *database.Tx, now temporal.DateTime, result *PruneResult) error {
	query := runs.Query().Where(orm.TimeLte(runs.RetainUntil, now)).
		OrderBy(orm.TimeAsc(runs.RetainUntil), orm.KeyAsc(runs.Key())).Limit(maxPruneRuns)
	outer := rows.From(query)
	inner := rows.From(flushes.Query().Where(orm.TimeGt(flushes.RetainUntil, now)))
	live := rows.Correlate(outer, inner)
	live = live.Where(expr.Equal(
		expr.At(rows.InnerScope(live, rows.ModelScope(flushes.Query())), orm.ColumnExpr(flushes.RunID)),
		expr.At(rows.OuterScope(live, rows.ModelScope(query)), orm.KeyExpr(runs.Key())),
	))
	query = expr.Where(query, expr.Not(live.Exists()))
	keys, err := rows.Read(rows.SelectValue(query, orm.KeyExpr(runs.Key()))).All(ctx, tx)
	if err != nil || len(keys) == 0 {
		return err
	}
	metricCount, err := orm.Count(ctx, tx, metrics.Query().Where(orm.ColumnIn(metrics.RunID, keys...)))
	if err != nil {
		return err
	}
	receiptCount, err := orm.Count(ctx, tx, flushes.Query().Where(orm.ColumnIn(flushes.RunID, keys...)))
	if err != nil {
		return err
	}
	alertCount, err := orm.Count(ctx, tx, alerts.Query().Where(orm.ColumnIn(alerts.RunID, keys...)))
	if err != nil {
		return err
	}
	// Lowered capacities must not turn a fifty-run cascade into an unbounded
	// deletion. Ordinary pruning above still advances by at most 500 keys per
	// table until this candidate set fits the current child-table budgets.
	if metricCount > int64(s.config.Retention.MaxMetricRows) || receiptCount > int64(s.config.Retention.MaxReceiptRows) || alertCount > int64(s.config.Retention.MaxRuns)*int64(len(RuleValues())) {
		return nil
	}
	deleted, err := orm.DeleteAll(ctx, tx, runs.Query().Where(orm.KeyIn(runs.Key(), keys...)))
	if err != nil {
		return err
	}
	result.Runs += deleted
	result.Metrics += metricCount
	result.Receipts += receiptCount
	return nil
}
