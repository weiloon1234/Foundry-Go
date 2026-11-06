package telemetry

import (
	"context"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type incidentDecision struct {
	rule Rule
	bad  bool
}

type sampleIdentity struct{ name, labels string }

// incidentProgress evaluates one capture against confirmed receipt state.
// Minute buckets are cumulative: another capture in the same minute is another
// sustain/recovery observation, even when its histogram has not changed.
func (s *storage) incidentProgress(b batch) ([]ruleProgress, []incidentDecision, error) {
	policy := s.config.Alerts
	var count, failed uint64
	buckets := make([]uint64, len(observability.DurationBuckets()))
	for _, entry := range b.buckets {
		if entry.Interval != time.Minute || s.collector.config.Routes.excludesRoute(entry.Metric.Route) {
			continue
		}
		m := entry.Metric
		if len(m.Buckets) != len(buckets) || math.MaxUint64-count < m.Count {
			return nil, nil, corruptPayload()
		}
		count += m.Count
		if m.Result.Status >= 500 {
			failed += m.Count
		}
		for i, n := range m.Buckets {
			if math.MaxUint64-buckets[i] < n {
				return nil, nil, corruptPayload()
			}
			buckets[i] += n
		}
	}
	estimate, err := observability.EstimateQuantile(buckets, count, .95)
	if err != nil {
		return nil, nil, corruptPayload()
	}
	previousCounters := make(map[sampleIdentity]float64, len(s.progress.Counters))
	for _, sample := range s.progress.Counters {
		previousCounters[sampleIdentity{sample.Name, sample.Labels}] = sample.Value
	}
	logFailure := false
	for _, sample := range b.runtime.Samples {
		if sample.Name == "foundry_log_write_failures_total" && sample.Value > previousCounters[sampleIdentity{sample.Name, sample.Labels}] {
			logFailure = true
		}
	}
	authFailures := 0
	for _, issue := range b.issues {
		if issue.Source == SourceSecurity && (issue.Code == "authentication_failed" || issue.Code == "rate_limited") {
			authFailures++
		}
	}
	previous := make(map[Rule]ruleProgress, len(s.progress.Rules))
	for _, progress := range s.progress.Rules {
		previous[progress.Rule] = progress
	}
	next := make([]ruleProgress, 0, len(policy.Rules))
	decisions := make([]incidentDecision, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		progress := previous[rule]
		progress.Rule = rule
		if (rule == RuleLatency || rule == RuleHTTPErrors) && count < uint64(policy.Thresholds.MinimumRequests) {
			progress.Bad, progress.Healthy = 0, 0
			next = append(next, progress)
			continue
		}
		var bad bool
		switch rule {
		case RuleLatency:
			bad = estimate.Available && (estimate.Overflow || estimate.Seconds >= policy.Thresholds.Latency.Seconds())
		case RuleHTTPErrors:
			bad = float64(failed)/float64(max(uint64(1), count)) >= policy.Thresholds.ErrorRatio
		case RulePoolPressure:
			bad = b.poolUsage >= policy.Thresholds.PoolUsageRatio
		case RuleLogFailures:
			bad = logFailure
		case RuleTelemetryLoss:
			bad = b.runtime.Dropped > s.progress.Dropped || b.runtime.StorageFailures > s.progress.StorageFailures
		case RuleAuthenticationBurst:
			bad = authFailures >= policy.Thresholds.AuthFailures
		default:
			return nil, nil, corruptPayload()
		}
		if bad {
			progress.Bad, progress.Healthy = min(progress.Bad+1, policy.SustainCaptures), 0
		} else {
			progress.Bad, progress.Healthy = 0, min(progress.Healthy+1, policy.RecoverCaptures)
		}
		next = append(next, progress)
		if progress.Bad >= policy.SustainCaptures || progress.Healthy >= policy.RecoverCaptures {
			decisions = append(decisions, incidentDecision{rule: rule, bad: bad})
		}
	}
	return next, decisions, nil
}

func (s *storage) writeIncidents(ctx context.Context, tx *database.Tx, b batch) ([]ruleProgress, error) {
	next, decisions, err := s.incidentProgress(b)
	if err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		key := AlertKey(digest(s.collector.runID.String() + "|" + string(decision.rule)))
		found, err := orm.FirstLocked(ctx, tx, orm.Lock(alerts.Query()).Where(orm.KeyEq(alerts.Key(), key)))
		if err != nil {
			return nil, err
		}
		current, exists := found.Get()
		if !exists {
			if decision.bad {
				_, err = orm.Insert(ctx, tx, alerts.Table, orm.KeySet(alerts.Key(), key), orm.ColumnSet(alerts.RunID, s.runKey()), orm.EnumSet(alerts.Rule, decision.rule), orm.EnumSet(alerts.State, AlertStateOpen), orm.NumberSet(alerts.Episode, Episode(1)), orm.TimeSet(alerts.FirstAt, b.at), orm.TimeSet(alerts.LastAt, b.at), orm.NullableTimeSetNull(alerts.RecoveredAt))
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		at := b.at
		if at.UTC().Before(current.LastAt.UTC()) {
			at = current.LastAt
		}
		draft := []orm.Assignment[alertRow]{orm.TimeSet(alerts.LastAt, at)}
		if decision.bad && current.State == AlertStateRecovered {
			if current.Episode >= math.MaxInt64 {
				return nil, safeerror.NewFault(fault.Conflict, "telemetry incident episode exhausted")
			}
			draft = append(draft, orm.EnumSet(alerts.State, AlertStateOpen), orm.NumberSet(alerts.Episode, current.Episode+1), orm.TimeSet(alerts.FirstAt, at), orm.NullableTimeSetNull(alerts.RecoveredAt))
		} else if !decision.bad && current.State != AlertStateRecovered {
			draft = append(draft, orm.EnumSet(alerts.State, AlertStateRecovered), orm.NullableTimeSet(alerts.RecoveredAt, at))
		}
		if _, err := orm.Update(ctx, tx, alerts.Query(), alerts.Key(), key, draft...); err != nil {
			return nil, err
		}
	}
	return next, nil
}

// AcknowledgeIn locks and acknowledges the current incident episode in the
// caller's transaction on this engine's database. It performs no authorization:
// the caller must first lock/recheck its actor and persist attribution using the
// returned episode in that same transaction. Success does not promise commit.
// Recovered incidents conflict. Repeated acknowledgement returns the same episode.
func (e *Engine) AcknowledgeIn(ctx context.Context, tx *database.Tx, key AlertKey) (Acknowledgement, error) {
	if e == nil || e.storage == nil || e.deps.DB == nil || ctx == nil {
		return Acknowledgement{}, invalid("telemetry acknowledgement requires an engine and context")
	}
	if _, err := ParseAlertKey(string(key)); err != nil {
		return Acknowledgement{}, err
	}
	if err := ctx.Err(); err != nil {
		return Acknowledgement{}, err
	}
	if err := e.operations.enter(); err != nil {
		return Acknowledgement{}, err
	}
	defer e.operations.leave()
	var result Acknowledgement
	err := sqlscope.InSchema(ctx, tx, e.deps.DB, e.deps.Schema, func(child *database.Tx) error {
		current, err := orm.RequireLocked(ctx, child, orm.Lock(alerts.Query()).Where(orm.KeyEq(alerts.Key(), key)))
		if err != nil {
			return err
		}
		if current.State == AlertStateRecovered {
			return safeerror.NewFault(fault.Conflict, "telemetry incident has recovered")
		}
		if _, err := orm.Update(ctx, child, alerts.Query(), alerts.Key(), key, orm.EnumSet(alerts.State, AlertStateAcknowledged)); err != nil {
			return err
		}
		result = Acknowledgement{Key: key, Episode: current.Episode}
		return nil
	})
	if err != nil {
		return Acknowledgement{}, safeerror.Prefix("telemetry acknowledgement", err)
	}
	return result, nil
}
