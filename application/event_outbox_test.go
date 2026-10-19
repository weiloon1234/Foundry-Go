package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

func TestEventOutboxValidationPrecedesResourceConstruction(t *testing.T) {
	key := foundation.NewKey[*events.Outbox]("fixture.publications")
	other := foundation.NewKey[*events.Outbox]("fixture.other")
	declaration := application.EventOutbox(key, "publications")
	for _, tc := range []struct {
		name         string
		change       func(*application.Settings)
		declarations []application.EventDeclaration
		kind         error
		message      string
	}{
		{"events disabled", func(s *application.Settings) { s.Features.Events.Enabled = false }, nil, fault.Invalid, "features.events"},
		{"outbox disabled", func(s *application.Settings) { s.Features.Outbox.Enabled = false }, nil, fault.Invalid, "features.outbox"},
		{"unknown connection", func(s *application.Settings) { s.Features.Outbox.Events["publications"] = "unknown" }, nil, fault.Missing, "connection is not configured"},
		{"no connection", func(s *application.Settings) { s.Services.Jobs.Connections = nil }, nil, fault.Missing, "connection is not configured"},
		{"memory connection", func(s *application.Settings) {
			c := s.Services.Jobs.Connections["default"]
			c.Driver = infrastructure.MemoryJobs
			s.Services.Jobs.Connections["default"] = c
		}, nil, fault.Invalid, "durable"},
		{"sync connection", func(s *application.Settings) {
			c := s.Services.Jobs.Connections["default"]
			c.Driver = infrastructure.SyncJobs
			s.Services.Jobs.Connections["default"] = c
		}, nil, fault.Invalid, "durable"},
		{"undeclared destination", func(s *application.Settings) { s.Features.Outbox.Events["unknown"] = "default" }, nil, fault.Missing, "no EventOutbox declaration"},
		{"unconfigured destination", func(s *application.Settings) { s.Features.Outbox.Events = nil }, nil, fault.Missing, "no features.outbox.events"},
		{"duplicate destination", nil, []application.EventDeclaration{declaration, application.EventOutbox(other, "publications")}, fault.Duplicate, "declared twice"},
		{"duplicate key", nil, []application.EventDeclaration{declaration, application.EventOutbox(key, "other")}, fault.Duplicate, "declared twice"},
		{"invalid destination", nil, []application.EventDeclaration{application.EventOutbox(key, "bad destination")}, fault.Invalid, "semantic name"},
		{"empty key", nil, []application.EventDeclaration{application.EventOutbox(foundation.Key[*events.Outbox]{}, "publications")}, fault.Invalid, "producer key"},
		{"invalid queue", func(s *application.Settings) {
			c := s.Services.Jobs.Connections["default"]
			c.DefaultQueue = ""
			s.Services.Jobs.Connections["default"] = c
		}, nil, fault.Invalid, "queue"},
		{"too many destinations", func(s *application.Settings) {
			for i := 0; i < 129; i++ {
				s.Features.Outbox.Events[outbox.Destination(fmt.Sprintf("destination.%d", i))] = "default"
			}
		}, nil, fault.Invalid, "too many"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := settings()
			s.HTTP.Enabled = false
			s.Features.Events.Enabled, s.Features.Outbox.Enabled = true, true
			c := infrastructure.DefaultJobConnectionSettings()
			c.Driver = infrastructure.RedisJobs
			s.Services.Jobs.Connections = infrastructure.JobConnections{"default": c}
			s.Features.Outbox.Events = map[outbox.Destination]jobs.ConnectionName{"publications": "default"}
			if tc.change != nil {
				tc.change(&s)
			}
			declarations := tc.declarations
			if declarations == nil {
				declarations = []application.EventDeclaration{declaration}
			}
			registered := false
			_, err := application.New(s, quiet()).Events(declarations...).Register(foundation.Module{Name: "fixture.unreached", OnRegister: func(*foundation.Registrar) error { registered = true; return nil }}).Build(t.Context())
			if !errors.Is(err, tc.kind) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %v containing %q, got %v", tc.kind, tc.message, err)
			}
			if registered {
				t.Fatal("invalid event outbox reached resource registration")
			}
		})
	}
}

func eventOutboxSettings() application.Settings {
	s := settings()
	s.HTTP.Enabled = false
	s.Features.Events.Enabled, s.Features.Outbox.Enabled = true, true
	db := infrastructure.DefaultConnectionSettings()
	db.Primary.Host, db.Primary.Database, db.Primary.User = "127.0.0.1", "fixture", "fixture"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": db}
	r := infrastructure.DefaultRedisConnectionSettings()
	r.Host = "127.0.0.1"
	s.Services.Redis.Connections = infrastructure.RedisConnections{"default": r}
	c := infrastructure.DefaultJobConnectionSettings()
	c.Driver = infrastructure.RedisJobs
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": c}
	s.Features.Outbox.Events = map[outbox.Destination]jobs.ConnectionName{"publications": "", "secondary": "default"}
	return s
}

func TestEventOutboxRegistrationSharesConnectionAndSnapshotsSettings(t *testing.T) {
	s := eventOutboxSettings()
	key := foundation.NewKey[*events.Outbox]("fixture.publications")
	other := foundation.NewKey[*events.Outbox]("fixture.secondary")
	declarations := []application.EventDeclaration{application.EventOutbox(key, "publications"), application.EventOutbox(other, "secondary")}
	firstBuilder := application.New(s, quiet()).Events(declarations...)
	secondBuilder := application.New(s, quiet()).Events(declarations...)
	s.Features.Outbox.Events["publications"] = "unknown"
	declarations[0] = application.EventDeclaration{}
	var producers []*events.Outbox
	for _, builder := range []*application.Builder{firstBuilder, secondBuilder} {
		app, err := builder.Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, app) })
		producer, err := application.Resolve(app.Resources(), key)
		if err != nil {
			t.Fatal(err)
		}
		alias, err := application.Resolve(app.Resources(), key)
		if err != nil || alias != producer || producer.Destination() != "publications" {
			t.Fatal("producer registration changed", err)
		}
		producers = append(producers, producer)
	}
	if producers[0] == producers[1] {
		t.Fatal("applications shared an event outbox")
	}
}

func TestEventOutboxRejectsAdvancedProducerAndJobCollisions(t *testing.T) {
	key := foundation.NewKey[*events.Outbox]("fixture.publications")
	for _, job := range []bool{false, true} {
		t.Run(fmt.Sprintf("job=%t", job), func(t *testing.T) {
			s := eventOutboxSettings()
			delete(s.Features.Outbox.Events, "secondary")
			builder := application.New(s, quiet()).Events(application.EventOutbox(key, "publications"))
			if job {
				definition := jobs.Define[assemblyPayload]("foundry.events.deliver", 1, jobs.DefaultPolicy("default"))
				builder.Jobs(application.Job(definition, func(application.Services) (jobs.Handler[assemblyPayload], error) {
					return func(context.Context, assemblyPayload) error { return nil }, nil
				}))
			} else {
				builder.Register(foundation.Module{Name: "fixture.advanced", Requires: []foundation.ProviderID{application.EventProvider}, OnRegister: func(r *foundation.Registrar) error {
					return events.RegisterOutbox(r, foundation.NewKey[*events.Outbox]("fixture.advanced"), application.EventKey, "publications")
				}})
			}
			if _, err := builder.Build(t.Context()); !errors.Is(err, fault.Duplicate) {
				t.Fatal("expected duplicate registration fault", err)
			}
		})
	}
}
