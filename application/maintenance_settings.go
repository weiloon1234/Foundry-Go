package application

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/maintenance/cachestore"
)

const MaintenanceProvider foundation.ProviderID = "foundry.application.maintenance"

// MaintenanceKey resolves the application's admission gate. It exists whether
// or not observability is enabled.
var MaintenanceKey = foundation.NewKey[*maintenance.Gate](string(MaintenanceProvider))

// MaintenanceStoreKey resolves the shared maintenance store when Store is set.
var MaintenanceStoreKey = foundation.NewKey[maintenance.Store](string(MaintenanceProvider) + ".store")

// MaintenanceSettings configure admission while the application is paused.
// Store names a configured cache store (Redis or PostgreSQL for a fleet) that
// shares operator state written by the down/up commands; every instance polls
// it every PollInterval. Empty Store keeps maintenance local to this process.
// Exempt rules ("[METHOD ]/path[/*]" as JSON Rule objects) and Allow networks
// admit matching requests while paused; draining still rejects everything.
type MaintenanceSettings struct {
	Store        cache.StoreName
	PollInterval time.Duration
	Exempt       []maintenance.Rule `config:",json"`
	Allow        []netip.Prefix     `config:",json"`
	BypassTTL    time.Duration
}

func DefaultMaintenanceSettings() MaintenanceSettings {
	return MaintenanceSettings{PollInterval: maintenance.DefaultPollInterval, BypassTTL: maintenance.DefaultBypassTTL}
}

func (s MaintenanceSettings) policy() maintenance.Policy {
	return maintenance.Policy{Exempt: slices.Clone(s.Exempt), Allow: slices.Clone(s.Allow), BypassTTL: s.BypassTTL}
}

func (s MaintenanceSettings) configured() bool {
	return len(s.Exempt) > 0 || len(s.Allow) > 0
}

// prepareMaintenance creates the one admission gate shared by every kernel and
// the recorder. Public probe paths stay reachable while paused.
func prepareMaintenance(s Settings, o options) (*maintenance.Gate, error) {
	m := s.Maintenance
	if m.Store != "" {
		store, ok := s.Services.Cache.Stores[m.Store]
		if !ok {
			return nil, fault.New(fault.Missing, "maintenance store is not a configured cache store")
		}
		// A null cache retains nothing, so a shared pause would silently vanish.
		if store.Driver == infrastructure.NullCache {
			return nil, fault.New(fault.Invalid, "maintenance store cannot use the null cache driver")
		}
		if m.PollInterval < maintenance.MinPollInterval || m.PollInterval > maintenance.MaxPollInterval {
			return nil, fault.New(fault.Invalid, "maintenance poll interval is out of range")
		}
	}
	policy := m.policy()
	if s.HTTP.Enabled {
		if err := s.HTTP.Probes.validate(); err != nil {
			return nil, err
		}
		for _, path := range s.HTTP.Probes.paths() {
			for _, rule := range []maintenance.Rule{{Method: "GET", Path: path}, {Method: "HEAD", Path: path}} {
				if !slices.Contains(policy.Exempt, rule) {
					policy.Exempt = append(policy.Exempt, rule)
				}
			}
		}
	}
	if o.recorder != nil {
		// A supplied recorder's gate keeps its owner's policy, including probes.
		if m.configured() {
			return nil, fault.New(fault.Invalid, "supplied recorder owns its maintenance policy")
		}
		return o.recorder.Gate(), nil
	}
	// Bypass cookies are sealed with the application keys so every instance
	// accepts them while the shared store holds only the secret's digest.
	keys, err := s.Encryption.keyring()
	if err != nil {
		return nil, err
	}
	policy.Keys = keys
	gate, err := maintenance.New(policy)
	if err != nil {
		return nil, err
	}
	if s.Features.Observability.Maintenance {
		if err := gate.Set(true); err != nil {
			return nil, err
		}
	}
	return gate, nil
}

// registerMaintenance provides the gate and, with a shared store, polls it for
// the application lifetime. A store outage retains the last applied state.
func registerMaintenance(builder *foundation.Builder, s MaintenanceSettings, gate *maintenance.Gate, logger *slog.Logger) {
	var requires []foundation.ProviderID
	if s.Store != "" {
		requires = append(requires, infrastructure.CacheProvider(s.Store))
	}
	builder.Register(foundation.Module{Name: MaintenanceProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Provide(r, MaintenanceKey, gate); err != nil {
			return err
		}
		if s.Store == "" {
			return nil
		}
		return foundation.Factory(r, MaintenanceStoreKey, func(resolver foundation.Resolver) (maintenance.Store, error) {
			store, err := foundation.Resolve(resolver, infrastructure.CacheKey(s.Store))
			if err != nil {
				return nil, err
			}
			return cachestore.New(store)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		if s.Store == "" {
			return nil
		}
		store, err := foundation.Resolve(r.Services(), MaintenanceStoreKey)
		if err != nil {
			return err
		}
		report := func(err error) {
			logger.WarnContext(r.Context(), "maintenance state refresh failed; retaining the last applied state", slog.Any("diagnostic", errordiag.Describe(err)))
		}
		// Apply fleet state before kernels admit work; an outage must not block boot.
		initial, cancel := context.WithTimeout(ctx, min(s.PollInterval, 5*time.Second))
		err = maintenance.Refresh(initial, gate, store)
		cancel()
		if err != nil && ctx.Err() == nil {
			report(err)
		}
		return r.Go("poll", func(ctx context.Context) error {
			return maintenance.Watch(ctx, gate, store, s.PollInterval, report)
		})
	}})
}

func (s Services) Maintenance() (*maintenance.Gate, error) { return Resolve(s, MaintenanceKey) }

// MaintenanceStore returns the configured shared store, or fault.Missing.
func (s Services) MaintenanceStore() (maintenance.Store, error) {
	return Resolve(s, MaintenanceStoreKey)
}
