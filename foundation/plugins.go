package foundation

import (
	"context"
	"fmt"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

// Plugin is an explicitly linked extension. Register and Manifest are pure
// declarations. Boot acquires resources; register each successful acquisition
// immediately with Runtime.OnShutdown, including resources acquired before a
// later boot failure. Shutdown is called once only after successful Boot.
type Plugin interface {
	Manifest() pluginmanifest.Manifest
	Register(*Registrar) error
	Boot(context.Context, *Runtime) error
	Shutdown(context.Context, *Runtime) error
}

// PluginProvider identifies a plugin's scope in the ordinary provider graph.
// Applications can depend on this ID without a redundant provider wrapper.
func PluginProvider(id pluginmanifest.ID) ProviderID { return ProviderID("plugin:" + string(id)) }

type pluginProvider struct {
	manifest pluginmanifest.Manifest
	plugin   Plugin
}

func (p pluginProvider) ID() ProviderID { return PluginProvider(p.manifest.ID) }
func (p pluginProvider) Dependencies() []ProviderID {
	result := make([]ProviderID, len(p.manifest.Dependencies))
	for i, dependency := range p.manifest.Dependencies {
		result[i] = PluginProvider(dependency.ID)
	}
	return result
}
func (p pluginProvider) Register(r *Registrar) error { return p.plugin.Register(r) }
func (p pluginProvider) Boot(ctx context.Context, r *Runtime) error {
	if err := p.plugin.Boot(ctx, r); err != nil {
		return err
	}
	// This internal cleanup has a separate identity from user resource names.
	// Registering cannot fail merely because a plugin chose a similar name.
	return r.onPluginShutdown(func(ctx context.Context) error { return p.plugin.Shutdown(ctx, r) })
}

// RegisterPlugin snapshots declarations and adds plugins to the same dependency
// and resource lifecycle as application providers. No hidden init or loader runs.
func (b *Builder) RegisterPlugin(plugins ...Plugin) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, extension := range plugins {
		if b.err != nil {
			break
		}
		if b.built {
			b.err = fault.New(fault.Closed, "builder has already been built")
			break
		}
		if nilValue(extension) {
			b.err = fault.New(fault.Invalid, "nil plugin")
			break
		}
		var manifest pluginmanifest.Manifest
		b.err = callback.Isolated("read plugin manifest", func() error {
			manifest = extension.Manifest().Snapshot()
			return manifest.Validate()
		})
		if b.err != nil {
			break
		}
		b.add(pluginProvider{manifest: manifest, plugin: extension}, false)
	}
	return b
}

func validatePlugins(entries []providerEntry) error {
	installed := make(map[pluginmanifest.ID]pluginmanifest.Manifest)
	type namespaceOwner struct {
		name  string
		owner pluginmanifest.ID
	}
	var namespaces []namespaceOwner
	for _, entry := range entries {
		if p, ok := entry.provider.(pluginProvider); ok {
			installed[p.manifest.ID] = p.manifest
			if namespace, err := p.manifest.Namespace(); err == nil {
				for _, previous := range namespaces {
					if namespace == previous.name || strings.HasPrefix(namespace, previous.name+".") || strings.HasPrefix(previous.name, namespace+".") {
						return fault.New(fault.Duplicate, fmt.Sprintf("plugin configuration namespace belongs to both %s and %s", previous.owner, p.manifest.ID))
					}
				}
				namespaces = append(namespaces, namespaceOwner{namespace, p.manifest.ID})
			}
		}
	}
	if len(installed) > 1024 {
		return fault.New(fault.Invalid, "plugin registration capacity exceeded")
	}
	for _, entry := range entries {
		p, ok := entry.provider.(pluginProvider)
		if !ok {
			continue
		}
		compatible, err := p.manifest.Framework.Accepts(pluginmanifest.FrameworkVersion)
		if err != nil {
			return err
		}
		if !compatible {
			return fault.New(fault.Conflict, "plugin "+string(p.manifest.ID)+" is incompatible with framework "+string(pluginmanifest.FrameworkVersion))
		}
		for _, dependency := range p.manifest.Dependencies {
			other, exists := installed[dependency.ID]
			if !exists {
				return fault.New(fault.Missing, fmt.Sprintf("plugin %s requires missing plugin %s", p.manifest.ID, dependency.ID))
			}
			compatible, err := dependency.Version.Accepts(other.Version)
			if err != nil {
				return err
			}
			if !compatible {
				return fault.New(fault.Conflict, fmt.Sprintf("plugin %s requires %s %s; installed version is %s", p.manifest.ID, dependency.ID, dependency.Version, other.Version))
			}
		}
	}
	return nil
}
