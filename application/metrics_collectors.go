package application

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

const MetricsProvider foundation.ProviderID = "foundry.application.metrics"

// registerMetricsCollectors exposes pool and realtime statistics through the
// application's recorder. Collectors read in-memory statistics only; labels
// are configured connection names and pool roles, never request data.
func registerMetricsCollectors(builder *foundation.Builder, recorder *observability.Recorder, logs *logging.ChannelSet, realtime bool) {
	if recorder == nil {
		return
	}
	requires := []foundation.ProviderID{Provider}
	if realtime {
		requires = append(requires, RealtimeProvider)
	}
	builder.Register(foundation.Module{Name: MetricsProvider, Requires: requires, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		services, err := FromResolver(r.Services())
		if err != nil {
			return err
		}
		if err := registerDatabaseCollector(recorder, services.Databases); err != nil {
			return err
		}
		if err := registerRedisCollector(recorder, services.Redis); err != nil {
			return err
		}
		if err := recorder.RegisterCollector("foundry.logging", loggingCollector(logs)); err != nil {
			return err
		}
		if err := registerJobsCollector(r, recorder, services.Jobs); err != nil {
			return err
		}
		if !realtime {
			return nil
		}
		hub, err := foundation.Resolve(r.Services(), RealtimeKey)
		if err != nil {
			return err
		}
		return recorder.RegisterCollector("foundry.realtime", realtimeCollector(hub))
	}})
}

func registerDatabaseCollector(recorder *observability.Recorder, connections *database.Connections) error {
	type named struct {
		name string
		db   *database.DB
	}
	var pools []named
	for _, name := range connections.Names() {
		if db, err := connections.Connection(name); err == nil {
			pools = append(pools, named{string(name), db})
		}
	}
	if len(pools) == 0 {
		return nil
	}
	return recorder.RegisterCollector("foundry.database", func(w *observability.MetricWriter) {
		for _, pool := range pools {
			stats := pool.db.RoutingStats()
			endpoints := []database.PoolStats{stats.Primary}
			if read, ok := stats.Read.Get(); ok {
				endpoints = append(endpoints, read)
			}
			for _, endpoint := range endpoints {
				connection, role := observability.Label{Name: "connection", Value: pool.name}, observability.Label{Name: "role", Value: string(endpoint.Role)}
				labels := []observability.Label{connection, role}
				for _, state := range []struct {
					name  string
					value int
				}{{"open", endpoint.Open}, {"in_use", endpoint.InUse}, {"idle", endpoint.Idle}} {
					w.Gauge("foundry_database_connections", "Database pool connections by state.", float64(state.value), connection, role, observability.Label{Name: "state", Value: state.name})
				}
				w.Gauge("foundry_database_max_open_connections", "Configured maximum open pool connections.", float64(endpoint.MaxOpen), labels...)
				w.Counter("foundry_database_waits_total", "Pool acquisitions that waited for a connection.", float64(endpoint.WaitCount), labels...)
				w.Counter("foundry_database_wait_seconds_total", "Total time spent waiting for pool connections.", endpoint.WaitDuration.Seconds(), labels...)
			}
			ready := 0.0
			if pool.db.Stats().Ready {
				ready = 1
			}
			w.Gauge("foundry_database_ready", "Whether the database connection is started and not closing.", ready, observability.Label{Name: "connection", Value: pool.name})
		}
	})
}

func registerRedisCollector(recorder *observability.Recorder, connections *redis.Connections) error {
	type named struct {
		name   string
		client *redis.Client
	}
	var clients []named
	for _, name := range connections.Names() {
		if client, err := connections.Connection(name); err == nil {
			clients = append(clients, named{string(name), client})
		}
	}
	if len(clients) == 0 {
		return nil
	}
	return recorder.RegisterCollector("foundry.redis", func(w *observability.MetricWriter) {
		for _, client := range clients {
			stats := client.client.Stats()
			label := observability.Label{Name: "connection", Value: client.name}
			w.Gauge("foundry_redis_connections", "Redis pool connections by state.", float64(stats.Open), label, observability.Label{Name: "state", Value: "open"})
			w.Gauge("foundry_redis_connections", "Redis pool connections by state.", float64(stats.Idle), label, observability.Label{Name: "state", Value: "idle"})
			w.Gauge("foundry_redis_active_operations", "Admitted unfinished Redis operations.", float64(stats.Operations), label)
			w.Gauge("foundry_redis_subscriptions", "Owned Redis pub/sub subscriptions.", float64(stats.Subscriptions), label)
			w.Gauge("foundry_redis_subscription_connections", "Dedicated Redis pub/sub connections.", float64(stats.SubscriptionConnections), label)
			ready := 0.0
			if stats.Ready {
				ready = 1
			}
			w.Gauge("foundry_redis_ready", "Whether the Redis connection is started and not closing.", ready, label)
		}
	})
}

func realtimeCollector(hub *websocket.Hub) observability.Collector {
	return func(w *observability.MetricWriter) {
		stats := hub.Snapshot()
		flag := func(value bool) float64 {
			if value {
				return 1
			}
			return 0
		}
		w.Gauge("foundry_realtime_connections", "Accepted WebSocket connections on this hub.", float64(stats.Connections))
		w.Gauge("foundry_realtime_subscriptions", "Active channel subscriptions on this hub.", float64(stats.Subscriptions))
		w.Gauge("foundry_realtime_active_operations", "Active hub publication and inspection operations.", float64(stats.ActiveOperations))
		w.Gauge("foundry_realtime_background_tasks", "Active hub background tasks.", float64(stats.BackgroundTasks))
		w.Gauge("foundry_realtime_degraded", "Whether the distributed hub is degraded.", flag(stats.Degraded))
		w.Gauge("foundry_realtime_stopping", "Whether the hub is stopping.", flag(stats.Stopping))
		w.Gauge("foundry_realtime_streaming", "Whether the fan-out stream is subscribed (always 1 for a local hub).", flag(stats.Streaming))
		w.Gauge("foundry_realtime_queued_bytes", "Bytes queued across this hub's connections.", float64(stats.QueuedBytes))
		for _, counter := range []struct {
			name, help string
			value      uint64
		}{
			{"foundry_realtime_accepted_total", "Accepted WebSocket upgrades.", stats.Accepted},
			{"foundry_realtime_rejected_total", "Rejected WebSocket upgrades.", stats.Rejected},
			{"foundry_realtime_publications_total", "Publications admitted by this hub.", stats.Publications},
			{"foundry_realtime_slow_consumers_total", "Connections closed because their queue filled.", stats.SlowConsumers},
			{"foundry_realtime_failures_total", "Failed hub operations.", stats.Failures},
			{"foundry_realtime_stream_gaps_total", "Lost fan-out streams.", stats.Gaps},
			{"foundry_realtime_resubscriptions_total", "Fan-out stream resubscriptions after a gap.", stats.Resubscriptions},
			{"foundry_realtime_dropped_envelopes_total", "Invalid cluster envelopes dropped.", stats.DroppedEnvelopes},
			{"foundry_realtime_operation_overloads_total", "Management operations rejected after waiting for capacity.", stats.OperationOverloads},
		} {
			w.Counter(counter.name, counter.help, float64(counter.value))
		}
	}
}

// loggingCollector surfaces sink delivery failures that slog callers cannot
// observe, including stderr fallbacks, drops and rotation/retention failures.
func loggingCollector(logs *logging.ChannelSet) observability.Collector {
	return func(w *observability.MetricWriter) {
		for _, channel := range logs.Stats() {
			label := observability.Label{Name: "channel", Value: string(channel.Channel)}
			for _, counter := range []struct {
				name, help string
				value      uint64
			}{
				{"foundry_log_records_total", "Records written by a log channel sink.", channel.Sink.Records},
				{"foundry_log_write_failures_total", "Failed log sink writes.", channel.Sink.Failures},
				{"foundry_log_fallbacks_total", "Failed records rewritten to stderr.", channel.Sink.Fallbacks},
				{"foundry_log_dropped_total", "Records dropped by a full asynchronous sink.", channel.Sink.Dropped},
				{"foundry_log_rotation_failures_total", "Failed log file rollovers.", channel.Sink.RotationFailures},
				{"foundry_log_retention_failures_total", "Failed log archive retention cleanups.", channel.Sink.PruneFailures},
			} {
				w.Counter(counter.name, counter.help, float64(counter.value), label)
			}
			w.Gauge("foundry_log_queued_records", "Records queued by an asynchronous sink.", float64(channel.Sink.Queued), label)
		}
	}
}

// registerJobsCollector samples each job connection's default queue in the
// background (jobs.DepthMonitor) and exposes the latest readings; scrapes
// never call the queue authority. Backends without statistics are skipped.
// Only worker and scheduler processes sample (or an application started
// without selecting kernels), so HTTP replicas and CLI commands add no load.
func registerJobsCollector(r *foundation.Runtime, recorder *observability.Recorder, connections *jobs.Connections) error {
	if running := r.SelectedKernels(); len(running) > 0 && !slices.Contains(running, foundation.Worker) && !slices.Contains(running, foundation.Scheduler) {
		return nil
	}
	var targets []jobs.DepthTarget
	for _, name := range connections.Names() {
		connection, err := connections.Connection(name)
		if err != nil {
			continue
		}
		targets = append(targets, jobs.DepthTarget{Connection: name, Queue: connection.DefaultQueue(), Dispatcher: connection.Dispatcher()})
	}
	if len(targets) == 0 {
		return nil
	}
	monitor, err := jobs.NewDepthMonitor(jobs.DefaultDepthInterval, targets...)
	if err != nil {
		return err
	}
	if err := r.Go("jobs-depth", monitor.Run); err != nil {
		return err
	}
	return recorder.RegisterCollector("foundry.jobs", jobsCollector(monitor))
}

func jobsCollector(monitor *jobs.DepthMonitor) observability.Collector {
	return func(w *observability.MetricWriter) {
		for _, reading := range monitor.Snapshot() {
			if !reading.Available {
				continue
			}
			connection, queue := observability.Label{Name: "connection", Value: string(reading.Connection)}, observability.Label{Name: "queue", Value: string(reading.Queue)}
			for _, state := range []struct {
				name  string
				value int64
			}{{"waiting", reading.Stats.Waiting}, {"delayed", reading.Stats.Delayed}, {"blocked", reading.Stats.Blocked}, {"leased", reading.Stats.Leased}, {"failed", reading.Stats.Failed}, {"retained", reading.Stats.Retained}} {
				w.Gauge("foundry_jobs_queue_jobs", "Queue jobs by state at the last background sample.", float64(state.value), connection, queue, observability.Label{Name: "state", Value: state.name})
			}
			w.Gauge("foundry_jobs_queue_sampled_timestamp_seconds", "Unix time of the last successful queue depth sample.", float64(reading.SampledAt.Unix()), connection, queue)
		}
	}
}
