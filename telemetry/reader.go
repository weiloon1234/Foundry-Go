package telemetry

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Reader reads owned telemetry snapshots from the primary database. Calls enforce
// retention using PostgreSQL time, bounded concurrency, and the configured timeout.
// A full reader returns fault.Overloaded without queueing another database operation.
// The owning Engine closes admission; Reader never closes borrowed dependencies.
type Reader struct {
	db     *database.DB
	schema string
	config Config
	routes RoutePolicy
	owner  *operationOwner
	slots  chan struct{}
}

func newReader(deps Dependencies, config Config, routes RoutePolicy, owner *operationOwner) (*Reader, error) {
	if deps.DB == nil || !sqlname.Valid(deps.Schema) || owner == nil {
		return nil, invalid("telemetry reader requires a database, schema and owner")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := routes.Validate(); err != nil {
		return nil, err
	}
	config.Alerts.Rules = slices.Clone(config.Alerts.Rules)
	routes = (CollectorConfig{Routes: routes}).clone().Routes
	return &Reader{db: deps.DB, schema: deps.Schema, config: config, routes: routes, owner: owner, slots: make(chan struct{}, config.Reads.MaxConcurrent)}, nil
}

func (r *Reader) read(ctx context.Context, fn func(context.Context, *database.Tx, temporal.DateTime) error) error {
	if r == nil || r.db == nil || r.owner == nil || ctx == nil {
		return invalid("telemetry read requires a reader and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.owner.enter(); err != nil {
		return err
	}
	defer r.owner.leave()
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		return safeerror.NewFault(fault.Overloaded, "telemetry read capacity exhausted")
	}
	attempt, cancel := context.WithTimeout(ctx, r.config.Reads.Timeout)
	defer cancel()
	err := r.db.Primary().Transaction(attempt, func(tx *database.Tx) error {
		if _, err := tx.Exec(attempt, `SET LOCAL search_path TO "`+r.schema+`", pg_temp`); err != nil {
			return err
		}
		now, err := serverTime(attempt, tx)
		if err != nil {
			return err
		}
		return fn(attempt, tx, now)
	}, database.TxOptions{Isolation: database.RepeatableRead, ReadOnly: true})
	if ended := attempt.Err(); ended != nil {
		// Drivers may report cancellation or a broken connection instead of
		// DeadlineExceeded. The read's own context is the timeout authority.
		return safeerror.WrapFault(fault.Timeout, "telemetry read ended before completion", safeerror.Join("telemetry read interrupted", ended, err))
	}
	if err == nil {
		return nil
	}
	// Query hydration can reject malformed persisted JSON before decodePayload.
	// That is a storage failure, not an invalid client query.
	return safeerror.WrapFault(fault.Internal, "cannot read telemetry storage", err)
}

func (r *Reader) validatePage(page orm.PageRequest) error {
	if err := page.Validate(); err != nil {
		return err
	}
	if r == nil || page.Number > r.config.Reads.MaxPage || page.Size > r.config.Reads.MaxPageSize {
		return invalid("telemetry page exceeds configured limits")
	}
	return nil
}

// Overview returns retained runs and incidents. Window is validated but does not
// narrow their retention; OldestBucket ignores already expired metric buckets.
func (r *Reader) Overview(ctx context.Context, q OverviewQuery) (Overview, error) {
	if err := q.Window.Validate(); err != nil {
		return Overview{}, err
	}
	out := Overview{Runs: []Run{}, Alerts: []Alert{}}
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		hour, err := now.Add(-r.config.Retention.Hour)
		if err != nil {
			return err
		}
		found, err := orm.All(ctx, tx, runs.Query().Where(orm.TimeGte(runs.HeartbeatAt, hour)).OrderBy(orm.TimeDesc(runs.HeartbeatAt), orm.KeyAsc(runs.Key())).Limit(r.config.Reads.MaxOverviewRuns+1))
		if err != nil {
			return err
		}
		out.Truncated = len(found) > r.config.Reads.MaxOverviewRuns
		for _, row := range found[:min(len(found), r.config.Reads.MaxOverviewRuns)] {
			runtime, err := decodePayload(row.Runtime, maxRuntimePayloadBytes)
			if err != nil {
				return err
			}
			out.Runs = append(out.Runs, Run{ID: model.IDFromBytes[Run](row.ID.Bytes()), Instance: row.Instance, Release: row.Release, StartedAt: row.StartedAt, HeartbeatAt: row.HeartbeatAt, Stale: now.UTC().Sub(row.HeartbeatAt.UTC()) > 3*r.config.FlushInterval, Runtime: runtime})
		}
		incidents, err := orm.All(ctx, tx, retainedAlerts(hour).OrderBy(orm.TimeDesc(alerts.LastAt), orm.KeyAsc(alerts.Key())).Limit(r.config.Reads.MaxOverviewAlerts+1))
		if err != nil {
			return err
		}
		out.Truncated = out.Truncated || len(incidents) > r.config.Reads.MaxOverviewAlerts
		for _, row := range incidents[:min(len(incidents), r.config.Reads.MaxOverviewAlerts)] {
			out.Alerts = append(out.Alerts, readAlert(row))
		}
		minute, err := now.Add(-r.config.Retention.Minute)
		if err != nil {
			return err
		}
		oldest, err := rows.Read(rows.SelectValue(metrics.Query().Where(orm.Or(
			orm.And(orm.NumberEq(metrics.IntervalSeconds, 60), orm.TimeGte(metrics.BucketAt, minute)),
			orm.And(orm.NumberEq(metrics.IntervalSeconds, 3600), orm.TimeGte(metrics.BucketAt, hour)),
		)).OrderBy(orm.TimeAsc(metrics.BucketAt), orm.KeyAsc(metrics.Key())).Limit(1), orm.TimeExpr(metrics.BucketAt))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(oldest) > 0 {
			out.OldestBucket = value.Set(oldest[0])
		}
		return nil
	})
	if err != nil {
		return Overview{}, err
	}
	return out, nil
}

// Issues returns a stable page of one source, enforcing its configured retention
// even when the physical pruning job has not run.
func (r *Reader) Issues(ctx context.Context, q IssueQuery) (orm.PageResult[Issue], error) {
	if err := q.Source.Validate(); err != nil {
		return orm.PageResult[Issue]{}, err
	}
	if err := r.validatePage(q.Page); err != nil {
		return orm.PageResult[Issue]{}, err
	}
	var out orm.PageResult[Issue]
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		query, err := r.retainedIssues(now)
		if err != nil {
			return err
		}
		page, err := orm.Paginate(ctx, tx, query.Where(orm.EnumEq(issues.Source, q.Source)).OrderBy(orm.TimeDesc(issues.LastAt), orm.KeyAsc(issues.Key())), q.Page)
		if err != nil {
			return err
		}
		out = pageMetadata[Issue](page)
		for _, row := range page.Items {
			item, err := readIssue(row)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		return nil
	})
	if err != nil {
		return orm.PageResult[Issue]{}, err
	}
	return out, nil
}

// Issue returns one retained issue by fingerprint, or database.NotFound when
// it is unknown or expired. It shares the primary read, timeout, concurrency and
// shutdown admission rules of Issues and returns an owned detail snapshot.
func (r *Reader) Issue(ctx context.Context, fingerprint Fingerprint) (Issue, error) {
	if _, err := ParseFingerprint(string(fingerprint)); err != nil {
		return Issue{}, err
	}
	var out Issue
	var found bool
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		query, err := r.retainedIssues(now)
		if err != nil {
			return err
		}
		selected, err := orm.First(ctx, tx, query.Where(orm.KeyEq(issues.Key(), fingerprint)))
		if err != nil {
			return err
		}
		row, ok := selected.Get()
		found = ok
		if !ok {
			return nil
		}
		out, err = readIssue(row)
		return err
	})
	if err != nil {
		return Issue{}, err
	}
	if !found {
		return Issue{}, database.NewError("telemetry issue", database.NotFound)
	}
	return out, nil
}

func (r *Reader) retainedIssues(now temporal.DateTime) (orm.Query[issueRow], error) {
	cutoff, err := now.Add(-r.config.Retention.Issue)
	if err != nil {
		return orm.Query[issueRow]{}, err
	}
	security, err := now.Add(-r.config.Retention.Security)
	if err != nil {
		return orm.Query[issueRow]{}, err
	}
	return issues.Query().Where(orm.Or(
		orm.And(orm.EnumEq(issues.Source, SourceSecurity), orm.TimeGte(issues.LastAt, security)),
		orm.And(orm.EnumNe(issues.Source, SourceSecurity), orm.TimeGte(issues.LastAt, cutoff)),
	)), nil
}

func readIssue(row issueRow) (Issue, error) {
	detail, err := decodePayload(row.Detail, maxDetailPayloadBytes)
	if err != nil {
		return Issue{}, err
	}
	return Issue{Fingerprint: row.Fingerprint, Source: row.Source, Code: row.Code, Route: row.Route, Release: row.Release, Count: row.Count, FirstAt: row.FirstAt, LastAt: row.LastAt, Detail: detail}, nil
}

// Events returns retained samples for a fingerprint. Each sample's own source
// selects its retention, independent of whether its issue group still exists.
func (r *Reader) Events(ctx context.Context, q EventQuery) (orm.PageResult[Event], error) {
	if _, err := ParseFingerprint(string(q.Fingerprint)); err != nil {
		return orm.PageResult[Event]{}, err
	}
	if err := r.validatePage(q.Page); err != nil {
		return orm.PageResult[Event]{}, err
	}
	var out orm.PageResult[Event]
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		cutoff, err := now.Add(-r.config.Retention.Issue)
		if err != nil {
			return err
		}
		security, err := now.Add(-r.config.Retention.Security)
		if err != nil {
			return err
		}
		query := events.Query().Where(orm.TextEq(events.Fingerprint, q.Fingerprint), orm.Or(
			orm.And(orm.EnumEq(events.Source, SourceSecurity), orm.TimeGte(events.CreatedAt, security)),
			orm.And(orm.EnumNe(events.Source, SourceSecurity), orm.TimeGte(events.CreatedAt, cutoff)),
		)).OrderBy(orm.TimeDesc(events.CreatedAt), orm.KeyAsc(events.Key()))
		page, err := orm.Paginate(ctx, tx, query, q.Page)
		if err != nil {
			return err
		}
		out = pageMetadata[Event](page)
		for _, row := range page.Items {
			detail, err := decodePayload(row.Detail, maxDetailPayloadBytes)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, Event{ID: model.IDFromBytes[Event](row.ID.Bytes()), CreatedAt: row.CreatedAt, Detail: detail})
		}
		return nil
	})
	if err != nil {
		return orm.PageResult[Event]{}, err
	}
	return out, nil
}

// Alerts returns a stable incident page whose owning runs remain within hour
// retention. An omitted state includes open, acknowledged and recovered episodes.
func (r *Reader) Alerts(ctx context.Context, q AlertQuery) (orm.PageResult[Alert], error) {
	if state, ok := q.State.Get(); ok {
		if err := state.Validate(); err != nil {
			return orm.PageResult[Alert]{}, err
		}
	}
	if err := r.validatePage(q.Page); err != nil {
		return orm.PageResult[Alert]{}, err
	}
	var out orm.PageResult[Alert]
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		cutoff, err := now.Add(-r.config.Retention.Hour)
		if err != nil {
			return err
		}
		query := retainedAlerts(cutoff).OrderBy(orm.TimeDesc(alerts.LastAt), orm.KeyAsc(alerts.Key()))
		if state, ok := q.State.Get(); ok {
			query = query.Where(orm.EnumEq(alerts.State, state))
		}
		page, err := orm.Paginate(ctx, tx, query, q.Page)
		if err != nil {
			return err
		}
		out = pageMetadata[Alert](page)
		for _, row := range page.Items {
			out.Items = append(out.Items, readAlert(row))
		}
		return nil
	})
	if err != nil {
		return orm.PageResult[Alert]{}, err
	}
	return out, nil
}

func retainedAlerts(cutoff temporal.DateTime) orm.Query[alertRow] {
	query := alerts.Query()
	parent := runs.Query().Where(orm.TimeGte(runs.HeartbeatAt, cutoff))
	live := rows.Correlate(rows.From(query), rows.From(parent))
	live = live.Where(expr.Equal(
		expr.At(rows.InnerScope(live, rows.ModelScope(parent)), orm.KeyExpr(runs.Key())),
		expr.At(rows.OuterScope(live, rows.ModelScope(query)), orm.ColumnExpr(alerts.RunID)),
	))
	return expr.Where(query, live.Exists())
}

func readAlert(row alertRow) Alert {
	return Alert{Key: row.Key, RunID: model.IDFromBytes[Run](row.RunID.Bytes()), Episode: row.Episode, Rule: row.Rule, State: row.State, FirstAt: row.FirstAt, LastAt: row.LastAt, RecoveredAt: row.RecoveredAt}
}

func pageMetadata[T, U any](page orm.PageResult[U]) orm.PageResult[T] {
	return orm.PageResult[T]{Items: make([]T, 0, len(page.Items)), Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages}
}
