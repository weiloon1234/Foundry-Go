package telemetry

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type bucket struct {
	Key      string
	At       time.Time
	Interval time.Duration
	Path     string
	Metric   http.RequestMetric
}

// batch owns its contents permanently. Storage consumes copies from accessors,
// retaining this identity at the FIFO head until receipt reconciliation. No
// accessor exposes a retained slice, and later captures never mutate this value.
type batch struct {
	id model.ID[batch]
	at temporal.DateTime
	// admitted retains time.Now's monotonic reading; it is never persisted.
	admitted  time.Time
	buckets   []bucket
	issues    []observedIssue
	runtime   Runtime
	poolUsage float64
}

func (b batch) bucketSnapshot() []bucket {
	result := slices.Clone(b.buckets)
	for i := range result {
		result[i].Metric.Buckets = slices.Clone(result[i].Metric.Buckets)
	}
	return result
}

func (b batch) issueSnapshot() []observedIssue {
	result := slices.Clone(b.issues)
	for i := range result {
		result[i].Detail.Frames = slices.Clone(result[i].Detail.Frames)
	}
	return result
}

func (b batch) runtimeSnapshot() Runtime {
	result := b.runtime
	result.Samples = slices.Clone(result.Samples)
	result.Health.Dependencies = slices.Clone(result.Health.Dependencies)
	return result
}

// capture is serialized by one owner, including synchronous recorder callbacks.
// It generates identity before draining, so a clock/UUID failure retains intake.
// Repeated captures without observations reproduce cumulative bucket counts,
// never add them back. Full queues reject the newest capture, not the head.
func (c *Collector) capture(recorder *observability.Recorder) error {
	return c.captureRuntime(recorder, Health{}, 0)
}

func (c *Collector) captureRuntime(recorder *observability.Recorder, health Health, poolUsage float64) error {
	c.captureMu.Lock()
	defer c.captureMu.Unlock()
	c.mu.Lock()
	c.admitted = true
	c.mu.Unlock()
	now, err := temporal.Now(c.clock)
	if err != nil {
		return err
	}
	id, err := model.NewIDAt[batch](now.UTC())
	if err != nil {
		return err
	}
	// Recorder owns callback isolation, finite sample validation and label escaping.
	samples, dropped := captureSamples(recorder.CollectSamples())
	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := c.elapsed()
	snapshot := c.metrics.Drain()
	c.dropLocked(snapshot.Dropped)
	c.dropLocked(dropped)
	c.expireLocked(elapsed)
	for key, b := range c.buckets {
		if b.At.Before(now.UTC().Truncate(b.Interval)) {
			delete(c.buckets, key)
		}
	}
	for _, metric := range snapshot.Series {
		for _, interval := range [...]time.Duration{time.Minute, time.Hour} {
			c.mergeLocked(now.UTC(), interval, metric)
		}
	}
	if len(c.pending) >= c.config.MaxPendingBatches {
		for _, metric := range snapshot.Series {
			c.dropLocked(metric.Count)
		}
		c.dropLocked(uint64(len(c.issues)))
		c.issues = nil
		clear(c.issueCounts)
		return nil
	}
	health.Dependencies = slices.Clone(health.Dependencies)
	b := batch{id: id, at: now, admitted: elapsed, issues: c.issues, poolUsage: poolUsage, runtime: Runtime{Health: health, Samples: samples, Dropped: c.dropped, StorageFailures: c.failures, Pending: len(c.pending)}}
	c.issues = nil
	clear(c.issueCounts)
	for _, entry := range c.buckets {
		entry.Metric.Buckets = slices.Clone(entry.Metric.Buckets)
		b.buckets = append(b.buckets, entry)
	}
	slices.SortFunc(b.buckets, func(a, b bucket) int { return strings.Compare(a.Key, b.Key) })
	c.pending = append(c.pending, b)
	return nil
}

func captureSamples(input []observability.MetricSample) ([]Sample, uint64) {
	samples := make([]Sample, 0, min(maxRuntimeSamples, len(input)))
	var dropped uint64
	for _, sample := range input {
		if len(samples) >= maxRuntimeSamples || len(sample.Labels) > maxSampleLabelBytes {
			dropped++
			continue
		}
		samples = append(samples, Sample{Name: strings.Clone(sample.Name), Labels: strings.Clone(sample.Labels), Value: sample.Value})
	}
	return samples, dropped
}

func (c *Collector) mergeLocked(now time.Time, interval time.Duration, metric http.RequestMetric) {
	at := now.Truncate(interval)
	key := digest(c.runID.String() + "|" + at.Format(time.RFC3339) + "|" + string(metric.Route) + "|" + string(metric.Method) + "|" + string(metric.Result.Outcome) + "|" + strconv.Itoa(metric.Result.Status) + "|" + strconv.FormatInt(int64(interval/time.Second), 10))
	b, exists := c.buckets[key]
	if !exists {
		if len(c.buckets) >= c.config.MaxSeries*2 {
			c.dropLocked(metric.Count)
			return
		}
		b = bucket{Key: key, At: at, Interval: interval, Path: c.paths[metric.Route], Metric: http.RequestMetric{Route: metric.Route, Method: metric.Method, Result: metric.Result, Buckets: make([]uint64, len(metric.Buckets))}}
	}
	// Reject an overflowing delta as a whole; never wrap counters or publish a
	// partial histogram that would produce a falsely healthy quantile.
	if ^uint64(0)-b.Metric.Count < metric.Count || math.IsInf(b.Metric.Seconds+metric.Seconds, 0) {
		c.dropLocked(metric.Count)
		return
	}
	for i, count := range metric.Buckets {
		if ^uint64(0)-b.Metric.Buckets[i] < count {
			c.dropLocked(metric.Count)
			return
		}
	}
	b.Metric.Count += metric.Count
	b.Metric.Seconds += metric.Seconds
	for i, count := range metric.Buckets {
		b.Metric.Buckets[i] += count
	}
	c.buckets[key] = b
}

func (c *Collector) expireLocked(now time.Time) {
	// Expiry uses monotonic elapsed time, never observation/database timestamps.
	retained := c.pending[:0]
	for _, b := range c.pending {
		if b.id != c.activeBatch && b.id != c.uncertainBatch && now.Sub(b.admitted) >= c.config.PendingRetention {
			c.dropBatchLocked(b)
			continue
		}
		retained = append(retained, b)
	}
	clear(c.pending[len(retained):])
	c.pending = retained
}
