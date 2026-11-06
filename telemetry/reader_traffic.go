package telemetry

import (
	"cmp"
	"context"
	"math"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type trafficGroup struct {
	route   http.RouteID
	path    string
	method  http.Method
	status  int
	outcome observability.Outcome
}

// Traffic sums cumulative histogram buckets before estimating quantiles. Windows
// through sixty minutes use minute buckets; larger windows use hour buckets and
// include the partial starting bucket, subject to the server retention cutoff.
// Truncated results summarize only admitted rows and groups.
func (r *Reader) Traffic(ctx context.Context, q TrafficQuery) (Traffic, error) {
	if err := q.Window.Validate(); err != nil {
		return Traffic{}, err
	}
	out := Traffic{Items: []TrafficRow{}}
	err := r.read(ctx, func(ctx context.Context, tx *database.Tx, now temporal.DateTime) error {
		interval, since, err := trafficWindow(now, q.Window, r.config.Retention)
		if err != nil {
			return err
		}
		groups := make(map[trafficGroup]store.Histogram)
		read := 0
		query := metrics.Query().Where(orm.NumberEq(metrics.IntervalSeconds, int(interval/time.Second)), orm.TimeGte(metrics.BucketAt, since)).OrderBy(orm.TimeAsc(metrics.BucketAt), orm.KeyAsc(metrics.Key())).Limit(r.config.Reads.MaxTrafficRows + 1)
		err = orm.Each(ctx, tx, query, func(row metricRow) error {
			read++
			if read > r.config.Reads.MaxTrafficRows {
				out.Truncated = true
				return nil
			}
			if !q.IncludeExcluded && r.routes.excludesRoute(row.Route) {
				return nil
			}
			key := trafficGroup{row.Route, row.RoutePath, row.Method, row.Status, row.Outcome}
			current, exists := groups[key]
			if !exists && len(groups) >= r.config.Reads.MaxTrafficGroups {
				out.Truncated = true
				return nil
			}
			histogram, err := decodePayload(row.Histogram, maxHistogramPayloadBytes)
			if err != nil {
				return err
			}
			if err := addReadHistogram(&current, histogram); err != nil {
				return err
			}
			groups[key] = current
			return nil
		})
		if err != nil {
			return err
		}
		var total store.Histogram
		for key, histogram := range groups {
			if err := addReadHistogram(&total, histogram); err != nil {
				return err
			}
			row := TrafficRow{Route: key.route, RoutePath: key.path, Method: key.method, Status: key.status, Outcome: key.outcome, Count: histogram.Count, Seconds: histogram.Seconds}
			row.P50, err = readQuantile(histogram, .5)
			if err != nil {
				return err
			}
			row.P95, err = readQuantile(histogram, .95)
			if err != nil {
				return err
			}
			row.P99, err = readQuantile(histogram, .99)
			if err != nil {
				return err
			}
			if row.Status >= 500 {
				out.Summary.Failed += row.Count
			}
			out.Items = append(out.Items, row)
		}
		out.Summary.Count = total.Count
		if len(groups) > 0 {
			out.Summary.P95, err = readQuantile(total, .95)
			if err != nil {
				return err
			}
		}
		slices.SortFunc(out.Items, func(a, b TrafficRow) int {
			return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Route, b.Route), cmp.Compare(a.RoutePath, b.RoutePath), cmp.Compare(a.Method, b.Method), cmp.Compare(a.Status, b.Status), cmp.Compare(a.Outcome, b.Outcome))
		})
		return nil
	})
	if err != nil {
		return Traffic{}, err
	}
	return out, nil
}

func trafficWindow(now temporal.DateTime, window Window, policy RetentionPolicy) (time.Duration, temporal.DateTime, error) {
	interval, retention := time.Minute, policy.Minute
	if window.Minutes > 60 {
		interval, retention = time.Hour, policy.Hour
	}
	start := now.UTC().Add(-time.Duration(window.Minutes) * time.Minute).Truncate(interval)
	if cutoff := now.UTC().Add(-retention); start.Before(cutoff) {
		start = cutoff
	}
	since, err := temporal.NewDateTime(start)
	return interval, since, err
}

func readQuantile(histogram store.Histogram, q float64) (observability.QuantileEstimate, error) {
	estimate, err := observability.EstimateQuantile(histogram.Buckets, histogram.Count, q)
	if err != nil {
		return observability.QuantileEstimate{}, corruptPayload()
	}
	return estimate, nil
}

func addReadHistogram(total *store.Histogram, next store.Histogram) error {
	if next.Seconds < 0 || math.IsNaN(next.Seconds) || math.IsInf(next.Seconds, 0) || (next.Count == 0 && next.Seconds != 0) {
		return corruptPayload()
	}
	if _, err := readQuantile(next, .5); err != nil {
		return err
	}
	if total.Count > math.MaxUint64-next.Count {
		return corruptPayload()
	}
	seconds := total.Seconds + next.Seconds
	if math.IsInf(seconds, 0) {
		return corruptPayload()
	}
	if total.Buckets == nil {
		total.Buckets = make([]uint64, len(next.Buckets))
	}
	if len(total.Buckets) != len(next.Buckets) {
		return corruptPayload()
	}
	for i, n := range next.Buckets {
		if total.Buckets[i] > math.MaxUint64-n {
			return corruptPayload()
		}
		total.Buckets[i] += n
	}
	total.Count += next.Count
	total.Seconds = seconds
	return nil
}
