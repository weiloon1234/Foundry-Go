package infrastructure

import (
	"cmp"
	"context"
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// RedisKeySpace describes one exact physical address prefix. Namespace components
// are validated and contain no Redis glob metacharacters. Prefix + "*" is an ACL
// pattern; never replace feature, application or environment with a wildcard.
type RedisKeySpace struct {
	Feature   keyspace.Feature   `json:"feature"`
	Namespace keyspace.Namespace `json:"namespace"`
	Version   string             `json:"version"`
	Prefix    string             `json:"prefix"`
}

// RedisConnectionInventory separates key permissions (~pattern) from pub/sub
// channel permissions (&pattern). It contains no connection configuration.
type RedisConnectionInventory struct {
	Connection redis.ConnectionName `json:"connection"`
	Keys       []RedisKeySpace      `json:"keys"`
	Channels   []RedisKeySpace      `json:"channels"`
}

// RedisKeys returns an owned, sorted and deduplicated snapshot of resolved Redis
// consumers. It performs no I/O. Unused connections and non-Redis stores add
// nothing. Arbitrary providers registered outside the plan are not inspectable;
// declare application stores with WithRedisStores to include them.
func (p *Plan) RedisKeys() []RedisConnectionInventory {
	result := make([]RedisConnectionInventory, 0)
	if p == nil {
		return result
	}
	connections := make(map[redis.ConnectionName]*RedisConnectionInventory)
	add := func(connection redis.ConnectionName, feature keyspace.Feature, namespace keyspace.Namespace, channel bool) {
		entry := connections[connection]
		if entry == nil {
			entry = &RedisConnectionInventory{Connection: connection, Keys: []RedisKeySpace{}, Channels: []RedisKeySpace{}}
			connections[connection] = entry
		}
		space := RedisKeySpace{Feature: feature, Namespace: namespace, Version: keyaddress.Version, Prefix: keyaddress.Prefix(feature, namespace)}
		if channel {
			entry.Channels = append(entry.Channels, space)
		} else {
			entry.Keys = append(entry.Keys, space)
		}
	}
	for _, c := range p.settings.Jobs.Connections {
		if c.Driver == RedisJobs {
			add(c.Redis, jobs.KeyFeature, c.Dispatch.Namespace, false)
		}
	}
	if c := p.settings.Coordination; c.Enabled && c.Driver == RedisCoordination {
		add(c.Redis, lease.KeyFeature, c.Config.Namespace, false)
	}
	for _, c := range p.settings.Cache.Stores {
		if c.Driver != RedisCache {
			continue
		}
		add(c.Redis, cache.KeyFeature, c.Config.Namespace, false)
		// Every Redis cache supports namespace invalidation, even when tags are
		// not required at construction. Include its reserved generation key.
		add(c.Redis, cache.NamespaceKeyFeature, c.Config.Namespace, false)
		if c.Require.DistributedFills {
			add(c.Redis, lease.KeyFeature, c.Leases.Namespace, false)
		}
	}
	for _, c := range p.settings.PubSub.Connections {
		if c.Driver == RedisBroker {
			add(c.Redis, pubsub.ChannelFeature, c.Config.Namespace, true)
		}
	}
	for _, c := range p.settings.Realtime.Connections {
		if c.Driver != RedisRealtime {
			continue
		}
		add(c.Redis, websocket.KeyFeature, c.Cluster.Namespace, false)
		add(c.Redis, pubsub.ChannelFeature, c.Cluster.Namespace, true)
	}
	for _, store := range p.redisStores {
		add(store.connection, store.feature, store.namespace, false)
	}
	canonical := func(spaces []RedisKeySpace) []RedisKeySpace {
		slices.SortFunc(spaces, func(a, b RedisKeySpace) int { return cmp.Compare(a.Prefix, b.Prefix) })
		return slices.Compact(spaces)
	}
	for _, name := range keys(connections) {
		entry := connections[name]
		entry.Keys, entry.Channels = canonical(entry.Keys), canonical(entry.Channels)
		result = append(result, *entry)
	}
	return result
}

// Jobs returns an owned snapshot of the plan's resolved job connection settings.
func (p *Plan) Jobs() JobsSettings {
	if p == nil {
		return JobsSettings{}
	}
	return JobsSettings{
		Default:     p.settings.Jobs.Default,
		Connections: maps.Clone(p.settings.Jobs.Connections),
	}
}

// RedisStorageID returns the physical storage digest for a configured Redis connection.
func (p *Plan) RedisStorageID(name redis.ConnectionName) (string, error) {
	if p == nil {
		return "", safeerror.NewFault(fault.Invalid, "Redis storage identity requires an infrastructure plan")
	}
	conn, ok := p.settings.Redis.Connections[name]
	if !ok {
		return "", safeerror.NewFault(fault.Missing, "Redis connection is not configured")
	}
	return conn.Config().StorageID()
}

// JobConnectionLayoutResult holds read-only layout inspection outcomes for one job connection.
type JobConnectionLayoutResult struct {
	Queues          map[jobs.Queue]redis.JobLayoutInspection
	QueueErrors     map[jobs.Queue]error
	ConnectionError error
}

// InspectJobLayouts opens only the Redis pools referenced by targets, inspects
// each declared queue layout without mutating Redis state, and closes the pools.
func (p *Plan) InspectJobLayouts(ctx context.Context, targets map[jobs.ConnectionName][]jobs.Queue) map[jobs.ConnectionName]JobConnectionLayoutResult {
	results := make(map[jobs.ConnectionName]JobConnectionLayoutResult, len(targets))
	if p == nil || len(targets) == 0 {
		return results
	}
	byRedis := make(map[redis.ConnectionName][]jobs.ConnectionName)
	for _, connName := range keys(targets) {
		jobCfg, ok := p.settings.Jobs.Connections[connName]
		if !ok || jobCfg.Driver != RedisJobs {
			continue
		}
		byRedis[jobCfg.Redis] = append(byRedis[jobCfg.Redis], connName)
	}
	for _, redisName := range keys(byRedis) {
		connNames := byRedis[redisName]
		redisCfg, ok := p.settings.Redis.Connections[redisName]
		if !ok {
			err := safeerror.NewFault(fault.Missing, "job Redis connection is not configured")
			for _, connName := range connNames {
				results[connName] = JobConnectionLayoutResult{ConnectionError: err}
			}
			continue
		}
		client, err := redis.Prepare(redisCfg.Config())
		if err != nil {
			for _, connName := range connNames {
				results[connName] = JobConnectionLayoutResult{ConnectionError: err}
			}
			continue
		}
		startErr := client.Start(ctx)
		if startErr != nil {
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisCfg.OperationTimeout)
			_ = client.Close(closeCtx)
			cancel()
			for _, connName := range connNames {
				results[connName] = JobConnectionLayoutResult{ConnectionError: startErr}
			}
			continue
		}
		for _, connName := range connNames {
			jobCfg := p.settings.Jobs.Connections[connName]
			res := JobConnectionLayoutResult{
				Queues:      make(map[jobs.Queue]redis.JobLayoutInspection, len(targets[connName])),
				QueueErrors: make(map[jobs.Queue]error),
			}
			backend, err := redis.NewJobBackend(client, jobCfg.Queue)
			if err != nil {
				res.ConnectionError = err
				results[connName] = res
				continue
			}
			for _, queue := range targets[connName] {
				key, err := jobs.NewKey(jobCfg.Dispatch.Namespace, queue)
				if err != nil {
					res.QueueErrors[queue] = err
					continue
				}
				inspection, err := backend.InspectLayout(ctx, key)
				if err != nil {
					res.QueueErrors[queue] = err
					continue
				}
				res.Queues[queue] = inspection
			}
			results[connName] = res
		}
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisCfg.OperationTimeout)
		_ = client.Close(closeCtx)
		cancel()
	}
	return results
}
