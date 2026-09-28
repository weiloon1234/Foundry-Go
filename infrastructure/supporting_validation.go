package infrastructure

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func (p *Plan) namespace(configured keyspace.Namespace, name string) keyspace.Namespace {
	if configured != (keyspace.Namespace{}) {
		return configured
	}
	configured = p.settings.Namespace
	configured.Application += "." + name
	return configured
}
func (p *Plan) redisReference(name redis.ConnectionName) (redis.ConnectionName, error) {
	if name == "" {
		name = p.settings.Redis.Default
	}
	if _, ok := p.settings.Redis.Connections[name]; !ok {
		return name, fault.New(fault.Missing, "dependent Redis connection is not configured")
	}
	return name, nil
}
func (p *Plan) validateSupporting() error {
	s := &p.settings
	fields := SettingsConfigKeys()
	for _, err := range []error{
		selection(fields.Mail.Default, s.Mail.Default, s.Mail.Mailers),
		selection(fields.Jobs.Default, s.Jobs.Default, s.Jobs.Connections),
		selection(fields.HTTPClients.Default, s.HTTPClients.Default, s.HTTPClients.Clients),
		selection(fields.PubSub.Default, s.PubSub.Default, s.PubSub.Connections),
		selection(fields.Realtime.Default, s.Realtime.Default, s.Realtime.Connections),
	} {
		if err != nil {
			return err
		}
	}
	for _, name := range keys(s.Mail.Mailers) {
		c := s.Mail.Mailers[name]
		if err := c.Config.Validate(); err != nil {
			return err
		}
		if err := c.Config.From.Validate(); err != nil {
			return fault.New(fault.Invalid, "configured mailer requires a valid sender")
		}
		var provider credentials.Provider
		if c.Driver == SESMail {
			if c.API.Credentials == "" {
				c.API.Credentials = "default"
			}
			_, configured := s.Credentials[c.API.Credentials]
			if !configured && p.options.credentials[c.API.Credentials] == nil {
				return fault.New(fault.Missing, "mail credential source is not configured")
			}
			provider = credentials.ProviderFunc(func(context.Context) (credentials.Value, error) {
				return credentials.Value{}, fault.New(fault.Internal, "validation-only credentials")
			})
		}
		adapter, err := p.mailAdapter(c, provider)
		if err != nil {
			return err
		}
		if adapter.close != nil {
			if err := adapter.close(context.Background()); err != nil {
				return err
			}
		}
		s.Mail.Mailers[name] = c
	}
	for _, name := range keys(s.HTTPClients.Clients) {
		c := s.HTTPClients.Clients[name]
		if c.Config.Name != name {
			return fault.New(fault.Invalid, "HTTP client name differs from its configured selector")
		}
		if err := c.Config.Validate(); err != nil {
			return err
		}
	}
	for _, name := range keys(s.Jobs.Connections) {
		c := s.Jobs.Connections[name]
		c.Dispatch.Namespace = p.namespace(c.Dispatch.Namespace, "jobs."+string(name))
		for _, err := range []error{c.DefaultQueue.Validate(), c.Dispatch.Validate(), c.Queue.Validate()} {
			if err != nil {
				return err
			}
		}
		switch c.Driver {
		case MemoryJobs:
		case RedisJobs:
			var err error
			c.Redis, err = p.redisReference(c.Redis)
			if err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported jobs driver")
		}
		s.Jobs.Connections[name] = c
	}
	for _, name := range keys(s.PubSub.Connections) {
		c := s.PubSub.Connections[name]
		c.Config.Namespace = p.namespace(c.Config.Namespace, "pubsub."+string(name))
		if err := c.Config.Validate(); err != nil {
			return err
		}
		switch c.Driver {
		case MemoryBroker:
		case RedisBroker:
			var err error
			c.Redis, err = p.redisReference(c.Redis)
			if err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported pub/sub driver")
		}
		s.PubSub.Connections[name] = c
	}
	for _, name := range keys(s.Realtime.Connections) {
		c := s.Realtime.Connections[name]
		c.Cluster.Namespace = p.namespace(c.Cluster.Namespace, "realtime."+string(name))
		switch c.Driver {
		case LocalRealtime:
		case RedisRealtime:
			if err := c.Cluster.Validate(); err != nil {
				return err
			}
			var err error
			c.Redis, err = p.redisReference(c.Redis)
			if err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported realtime driver")
		}
		s.Realtime.Connections[name] = c
	}
	if s.Coordination.Enabled {
		c := s.Coordination
		c.Config.Namespace = p.namespace(c.Config.Namespace, "coordination")
		if err := c.Config.Validate(); err != nil {
			return err
		}
		switch c.Driver {
		case MemoryCoordination:
			if c.MaxEntries < 1 || c.MaxEntries > 1<<20 {
				return fault.New(fault.Invalid, "invalid local lease capacity")
			}
		case RedisCoordination:
			var err error
			c.Redis, err = p.redisReference(c.Redis)
			if err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported coordination driver")
		}
		s.Coordination = c
	}
	return nil
}
