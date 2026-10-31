// Package plugin assembles explicitly linked Go extensions using the ordinary
// foundation registrar, typed feature APIs and managed application lifecycle.
package plugin

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

// ID is a plugin manifest identifier.
type ID = manifest.ID

// Version is a manifest release version.
type Version = manifest.Version

// Requirement is a manifest version requirement.
type Requirement = manifest.Requirement

// Dependency is a plugin manifest dependency.
type Dependency = manifest.Dependency

// Manifest is a plugin's manifest declaration.
type Manifest = manifest.Manifest

// Plugin is the foundation plugin lifecycle contract.
type Plugin = foundation.Plugin

// Registrar is the foundation declaration registrar.
type Registrar = foundation.Registrar

const FrameworkVersion = manifest.FrameworkVersion

// Module adapts optional callbacks to a plugin. Configuration belongs to the
// constructor's typed values; callbacks must not mutate shared module globals.
type Module struct {
	Declaration Manifest
	OnRegister  func(*Registrar) error
	OnBoot      func(context.Context, *foundation.Runtime) error
	OnShutdown  func(context.Context, *foundation.Runtime) error
}

// Manifest returns a snapshot of the module's manifest.
func (m Module) Manifest() Manifest { return m.Declaration.Snapshot() }

// Register calls OnRegister when supplied; otherwise it succeeds.
func (m Module) Register(r *Registrar) error {
	if m.OnRegister != nil {
		return m.OnRegister(r)
	}
	return nil
}

// Boot calls OnBoot when supplied; otherwise it succeeds.
func (m Module) Boot(ctx context.Context, r *foundation.Runtime) error {
	if m.OnBoot != nil {
		return m.OnBoot(ctx, r)
	}
	return nil
}

// Shutdown calls OnShutdown when supplied; otherwise it succeeds.
func (m Module) Shutdown(ctx context.Context, r *foundation.Runtime) error {
	if m.OnShutdown != nil {
		return m.OnShutdown(ctx, r)
	}
	return nil
}

// Provider returns the foundation provider identifier for a plugin.
func Provider(id ID) foundation.ProviderID { return foundation.PluginProvider(id) }
