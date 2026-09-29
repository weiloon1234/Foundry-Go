package application

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"slices"
)

const OutboxProvider foundation.ProviderID = "foundry.application.outbox"

var OutboxKey = foundation.NewKey[*publisher.Publisher](string(OutboxProvider))
var jobOutboxesKey = foundation.NewKey[map[jobs.ConnectionName]*jobs.Outbox](string(OutboxProvider) + ".jobs")

// schemaTransactor preserves publisher transactions while selecting the exact
// configured schema. sqlscope contains and restores search_path in a savepoint.
type schemaTransactor struct {
	db     *database.DB
	schema string
}

func (s schemaTransactor) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return s.db.Transaction(ctx, func(tx *database.Tx) error { return sqlscope.InSchema(ctx, tx, s.db, s.schema, fn) }, options...)
}
func registerOutbox(builder *foundation.Builder, s OutboxSettings, source clock.Clock) error {
	const producerProvider foundation.ProviderID = "foundry.application.outbox-producers"
	for i, kind := range s.Kernels {
		switch kind {
		case foundation.HTTP, foundation.CLI, foundation.Worker, foundation.Scheduler, foundation.WebSocket:
		default:
			return fault.New(fault.Invalid, "outbox publisher kernels must name known kernels")
		}
		if slices.Contains(s.Kernels[:i], kind) {
			return fault.New(fault.Duplicate, "outbox publisher kernel is listed more than once")
		}
	}
	requires := []foundation.ProviderID{infrastructure.DatabaseProvider(s.Database)}
	names := make([]jobs.ConnectionName, 0, len(s.Jobs))
	for name := range s.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		requires = append(requires, infrastructure.JobProvider(name))
	}
	builder.Register(foundation.Module{Name: producerProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, jobOutboxesKey, func(r foundation.Resolver) (map[jobs.ConnectionName]*jobs.Outbox, error) {
			db, err := foundation.Resolve(r, infrastructure.DatabaseKey(s.Database))
			if err != nil {
				return nil, err
			}
			result := make(map[jobs.ConnectionName]*jobs.Outbox, len(names))
			for _, name := range names {
				connection, err := foundation.Resolve(r, infrastructure.JobKey(name))
				if err != nil {
					return nil, err
				}
				producer, err := jobs.PrepareOutboxIn(s.Jobs[name], connection.Dispatcher(), db, s.Schema)
				if err != nil {
					return nil, err
				}
				if err := connection.Dispatcher().RequireDurable(); err != nil {
					return nil, err
				}
				result[name] = producer
			}
			return result, nil
		})
	}})
	// The module logs publication failures through the application logger and
	// runs the publisher only under the configured kernels.
	builder.Register(publisher.KernelModule(OutboxProvider, OutboxKey, s.Kernels, []foundation.ProviderID{producerProvider, FeatureDeclarationsProvider}, func(r foundation.Resolver) (*publisher.Publisher, error) {
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(s.Database))
		if err != nil {
			return nil, err
		}
		producers, err := foundation.Resolve(r, jobOutboxesKey)
		if err != nil {
			return nil, err
		}
		d, err := foundation.Resolve(r, featureDeclarationsKey)
		if err != nil {
			return nil, err
		}
		routes := slices.Clone(d.Outbox)
		for _, name := range names {
			route, err := producers[name].PublicationRoute()
			if err != nil {
				return nil, err
			}
			routes = append(routes, route)
		}
		return publisher.New(schemaTransactor{db, s.Schema}, s.runtime(source), routes...)
	}))
	return nil
}
func (s Services) OutboxPublisher() (*publisher.Publisher, error) { return Resolve(s, OutboxKey) }

// JobOutbox selects the named job connection's configured durable producer.
// Empty name selects the configured default connection, never another producer.
func (s Services) JobOutbox(name jobs.ConnectionName) (*jobs.Outbox, error) {
	if name == "" {
		if s.Services == nil || s.Jobs == nil {
			return nil, fault.New(fault.Missing, "jobs are not configured")
		}
		name = s.Jobs.DefaultName()
	}
	producers, err := Resolve(s, jobOutboxesKey)
	if err != nil {
		return nil, err
	}
	producer, ok := producers[name]
	if !ok {
		return nil, fault.New(fault.Missing, "job outbox is not configured")
	}
	return producer, nil
}
