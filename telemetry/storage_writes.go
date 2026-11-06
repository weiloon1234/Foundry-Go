package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func existingKeys[M any, K comparable](ctx context.Context, tx *database.Tx, table orm.Table[M, K], keys []K) (map[K]bool, error) {
	result := make(map[K]bool, len(keys))
	for len(keys) > 0 {
		n := min(len(keys), orm.MaxMembershipValues)
		selected, err := rows.Read(rows.SelectValue(table.Query().Where(orm.KeyIn(table.Key(), keys[:n]...)), orm.KeyExpr(table.Key()))).All(ctx, tx)
		if err != nil {
			return nil, err
		}
		for _, key := range selected {
			result[key] = true
		}
		keys = keys[n:]
	}
	return result, nil
}

func (s *storage) writeMetrics(ctx context.Context, tx *database.Tx, b batch) (uint64, error) {
	if len(b.buckets) == 0 {
		return 0, nil
	}
	count, err := orm.Count(ctx, tx, metrics.Query())
	if err != nil {
		return 0, err
	}
	keys := make([]store.MetricKey, len(b.buckets))
	for i, entry := range b.buckets {
		keys[i] = store.MetricKey(entry.Key)
	}
	existing, err := existingKeys(ctx, tx, metrics.Table, keys)
	if err != nil {
		return 0, err
	}
	var dropped uint64
	drafts := make([][]orm.Assignment[metricRow], 0, len(b.buckets))
	for _, entry := range b.buckets {
		key := store.MetricKey(entry.Key)
		if !existing[key] {
			if count >= int64(s.config.Retention.MaxMetricRows) {
				dropped = saturatingAdd(dropped, entry.Metric.Count)
				continue
			}
			count++
			existing[key] = true
		}
		at, err := temporal.NewDateTime(entry.At)
		if err != nil {
			return 0, err
		}
		m := entry.Metric
		hist, err := encodePayload(store.Histogram{Count: m.Count, Seconds: m.Seconds, Buckets: m.Buckets}, maxHistogramPayloadBytes)
		if err != nil {
			return 0, err
		}
		drafts = append(drafts, []orm.Assignment[metricRow]{orm.KeySet(metrics.Key(), key), orm.ColumnSet(metrics.RunID, s.runKey()), orm.TimeSet(metrics.BucketAt, at), orm.NumberSet(metrics.IntervalSeconds, int(entry.Interval/time.Second)), orm.TextSet(metrics.Route, m.Route), orm.TextSet(metrics.RoutePath, entry.Path), orm.TextSet(metrics.Method, m.Method), orm.NumberSet(metrics.Status, m.Result.Status), orm.TextSet(metrics.Outcome, m.Result.Outcome), orm.JSONSet(metrics.Histogram, hist)})
	}
	conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(metrics.Key()))).DoUpdate(orm.ConflictIncoming(orm.JSONExpr(metrics.Histogram)))
	for len(drafts) > 0 {
		n := min(len(drafts), orm.MaxInsertRows)
		if _, err := orm.UpsertMany(ctx, tx, metrics.Query(), conflict, drafts[:n]); err != nil {
			return 0, err
		}
		drafts = drafts[n:]
	}
	return dropped, nil
}

type orderedIssue struct {
	issue observedIssue
	index int
}

func (s *storage) writeIssues(ctx context.Context, tx *database.Tx, b batch) (uint64, error) {
	if len(b.issues) == 0 {
		return 0, nil
	}
	issueCount, err := orm.Count(ctx, tx, issues.Query())
	if err != nil {
		return 0, err
	}
	eventCount, err := orm.Count(ctx, tx, events.Query())
	if err != nil {
		return 0, err
	}
	keys := make([]Fingerprint, len(b.issues))
	ordered := make([]orderedIssue, len(b.issues))
	for i, issue := range b.issues {
		keys[i] = fingerprint(issue)
		ordered[i] = orderedIssue{issue: issue, index: i}
	}
	existing, err := existingKeys(ctx, tx, issues.Table, keys)
	if err != nil {
		return 0, err
	}
	slices.SortStableFunc(ordered, func(a, b orderedIssue) int { return a.issue.At.UTC().Compare(b.issue.At.UTC()) })
	groups := make(map[Fingerprint]issueRow)
	eventDrafts := make([][]orm.Assignment[eventRow], 0, len(ordered))
	var dropped uint64
	for _, entry := range ordered {
		issue := entry.issue
		key := fingerprint(issue)
		if !existing[key] {
			if issueCount >= int64(s.config.Retention.MaxIssueRows) {
				dropped = saturatingAdd(dropped, 1)
				continue
			}
			issueCount++
			existing[key] = true
		}
		detail, err := encodePayload(issue.Detail, maxDetailPayloadBytes)
		if err != nil {
			return 0, err
		}
		group, known := groups[key]
		if !known {
			group = issueRow{Fingerprint: key, Source: issue.Source, Code: issue.Code, Route: issue.Route, Release: issue.Release, FirstAt: issue.At, LastAt: issue.At, Detail: detail}
		}
		group.Count++
		if issue.At.UTC().Before(group.FirstAt.UTC()) {
			group.FirstAt = issue.At
		}
		if issue.At.UTC().After(group.LastAt.UTC()) {
			group.LastAt = issue.At
			group.Detail = detail
		}
		groups[key] = group
		if eventCount >= int64(s.config.Retention.MaxEventRows) {
			dropped = saturatingAdd(dropped, 1)
			continue
		}
		eventCount++
		eventDrafts = append(eventDrafts, []orm.Assignment[eventRow]{orm.KeySet(events.Key(), eventID(b.id, entry.index)), orm.TextSet(events.Fingerprint, key), orm.EnumSet(events.Source, issue.Source), orm.TimeSet(events.CreatedAt, issue.At), orm.JSONSet(events.Detail, detail)})
	}
	for len(eventDrafts) > 0 {
		n := min(len(eventDrafts), orm.MaxInsertRows)
		if _, err := orm.InsertMany(ctx, tx, events.Table, eventDrafts[:n]); err != nil {
			return 0, err
		}
		eventDrafts = eventDrafts[n:]
	}
	q := issues.Query()
	cr := rows.ConflictRowsOf(q)
	storedCount, proposedCount := expr.At(cr.Stored(), orm.NumberExpr(issues.Count)), expr.At(cr.Proposed(), orm.NumberExpr(issues.Count))
	storedFirst, proposedFirst := expr.At(cr.Stored(), orm.TimeExpr(issues.FirstAt)), expr.At(cr.Proposed(), orm.TimeExpr(issues.FirstAt))
	storedLast, proposedLast := expr.At(cr.Stored(), orm.TimeExpr(issues.LastAt)), expr.At(cr.Proposed(), orm.TimeExpr(issues.LastAt))
	storedDetail, proposedDetail := expr.At(cr.Stored(), orm.JSONExpr(issues.Detail)), expr.At(cr.Proposed(), orm.JSONExpr(issues.Detail))
	newer := expr.Greater(proposedLast, storedLast)
	conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(issues.Key()))).DoUpdate(rows.SetConflictValue(orm.NumberExpr(issues.Count), expr.Add(storedCount, proposedCount)), rows.SetConflictValue(orm.TimeExpr(issues.FirstAt), expr.When(expr.Less(proposedFirst, storedFirst), proposedFirst).Else(storedFirst)), rows.SetConflictValue(orm.TimeExpr(issues.LastAt), expr.When(newer, proposedLast).Else(storedLast)), rows.SetConflictValue(orm.JSONExpr(issues.Detail), expr.When(newer, proposedDetail).Else(storedDetail)))
	keys = keys[:0]
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	drafts := make([][]orm.Assignment[issueRow], 0, len(groups))
	for _, key := range keys {
		g := groups[key]
		drafts = append(drafts, []orm.Assignment[issueRow]{orm.KeySet(issues.Key(), key), orm.EnumSet(issues.Source, g.Source), orm.TextSet(issues.Code, g.Code), orm.TextSet(issues.Route, g.Route), orm.TextSet(issues.Release, g.Release), orm.NumberSet(issues.Count, g.Count), orm.TimeSet(issues.FirstAt, g.FirstAt), orm.TimeSet(issues.LastAt, g.LastAt), orm.JSONSet(issues.Detail, g.Detail)})
	}
	for len(drafts) > 0 {
		n := min(len(drafts), orm.MaxInsertRows)
		if _, err := orm.UpsertMany(ctx, tx, q, conflict, drafts[:n]); err != nil {
			return 0, err
		}
		drafts = drafts[n:]
	}
	return dropped, nil
}

// UUIDv8 event identities are deterministic within a UUIDv7 receipt and input
// position. Retrying an absent receipt retains every sample's identity too.
func eventID(id model.ID[batch], index int) model.ID[eventRow] {
	var input [24]byte
	bytes := id.Bytes()
	copy(input[:16], bytes[:])
	binary.BigEndian.PutUint64(input[16:], uint64(index))
	sum := sha256.Sum256(input[:])
	var result [16]byte
	copy(result[:], sum[:16])
	result[6] = (result[6] & 15) | 0x80
	result[8] = (result[8] & 63) | 0x80
	return model.IDFromBytes[eventRow](result)
}
