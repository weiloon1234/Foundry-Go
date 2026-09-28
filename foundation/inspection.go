package foundation

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

type contributionOverride struct {
	owner    ProviderID
	register func(*Registrar) error
}

// OverrideContributions explicitly replaces existing typed contributions after
// every ordinary registration, retaining each contribution's original position.
// All registrations in this callback must replace an existing contribution of
// exactly the same type. Missing targets and repeated replacements are errors.
// The owner is an application scope, never a plugin scope.
func (b *Builder) OverrideContributions(owner ProviderID, register func(*Registrar) error) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b
	}
	if b.built {
		b.err = fault.New(fault.Closed, "builder has already been built")
		return b
	}
	if !validName(string(owner)) || strings.HasPrefix(string(owner), "plugin:") || register == nil {
		b.err = fault.New(fault.Invalid, "invalid application contribution override")
		return b
	}
	b.overrides = append(b.overrides, contributionOverride{owner: owner, register: register})
	return b
}

// ContributionInfo describes declaration ownership without constructing or
// formatting the service value. Replaces retains the previous owner on override.
type ContributionInfo struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	Schema   string     `json:"schema,omitempty"`
	Owner    ProviderID `json:"owner"`
	Replaces ProviderID `json:"replaces,omitempty"`
}

type ProviderInfo struct {
	ID           ProviderID   `json:"id"`
	Dependencies []ProviderID `json:"dependencies,omitempty"`
}

// Inspection is an owned declaration snapshot. It contains no service values,
// configuration values, callback references or infrastructure connections.
type Inspection struct {
	Providers     []ProviderInfo            `json:"providers"`
	Plugins       []pluginmanifest.Manifest `json:"plugins"`
	Contributions []ContributionInfo        `json:"contributions"`
}

func (i Inspection) snapshot() Inspection {
	i.Providers = slices.Clone(i.Providers)
	for n := range i.Providers {
		i.Providers[n].Dependencies = slices.Clone(i.Providers[n].Dependencies)
	}
	i.Plugins = slices.Clone(i.Plugins)
	for n := range i.Plugins {
		i.Plugins[n] = i.Plugins[n].Snapshot()
	}
	i.Contributions = slices.Clone(i.Contributions)
	return i
}

// Inspect validates the graph and runs pure registration callbacks on a fresh
// registry. It never invokes service constructors, Boot, Shutdown or kernels,
// and does not consume the builder. Register callbacks must be repeatable.
func (b *Builder) Inspect(ctx context.Context) (Inspection, error) {
	b.mu.Lock()
	entries, overrides, err := slices.Clone(b.providers), slices.Clone(b.overrides), b.err
	b.mu.Unlock()
	if err != nil {
		return Inspection{}, err
	}
	ordered, registry, err := registerProviders(ctx, entries, overrides)
	if err != nil {
		return Inspection{}, err
	}
	return inspectRegistry(ordered, registry), nil
}

func registerProviders(ctx context.Context, entries []providerEntry, overrides []contributionOverride) ([]providerEntry, *registry, error) {
	if ctx == nil {
		return nil, nil, fault.New(fault.Invalid, "nil assembly context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := validatePlugins(entries); err != nil {
		return nil, nil, err
	}
	ordered, err := orderProviders(entries)
	if err != nil {
		return nil, nil, err
	}
	registry := newRegistry()
	defer registry.freeze()
	for _, entry := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		registrar := &Registrar{registry: registry, owner: entry.id}
		if p, ok := entry.provider.(pluginProvider); ok {
			declaration := p.manifest.Snapshot()
			registrar.plugin = &declaration
		}
		if err := callback.Isolated("register provider "+string(entry.id), func() error { return entry.provider.Register(registrar) }); err != nil {
			return nil, nil, err
		}
	}
	for _, override := range overrides {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		r := &Registrar{registry: registry, owner: override.owner, override: true}
		if err := callback.Isolated("override contributions "+string(override.owner), func() error { return override.register(r) }); err != nil {
			return nil, nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return ordered, registry, nil
}

func inspectRegistry(entries []providerEntry, r *registry) Inspection {
	result := Inspection{Providers: []ProviderInfo{}, Plugins: []pluginmanifest.Manifest{}, Contributions: []ContributionInfo{}}
	for _, entry := range entries {
		result.Providers = append(result.Providers, ProviderInfo{entry.id, slices.Clone(entry.dependencies)})
		if p, ok := entry.provider.(pluginProvider); ok {
			result.Plugins = append(result.Plugins, p.manifest.Snapshot())
		}
	}
	for _, name := range r.order {
		definition := r.services[name]
		info := ContributionInfo{Name: name, Type: definition.ref.typ.String(), Owner: definition.owner, Replaces: definition.replaces}
		if definition.schema != nil {
			info.Schema = definition.schema.String()
		}
		result.Contributions = append(result.Contributions, info)
	}
	kinds := make([]KernelKind, 0, len(r.kernels))
	for kind := range r.kernels {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	for _, kind := range kinds {
		definition := r.kernels[kind]
		result.Contributions = append(result.Contributions, ContributionInfo{Name: string(kind), Type: "kernel", Owner: definition.owner, Replaces: definition.replaces})
	}
	return result
}
