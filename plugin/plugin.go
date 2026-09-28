// Package plugin assembles explicitly linked Go extensions using the ordinary
// foundation registrar, typed feature APIs and managed application lifecycle.
package plugin

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

type ID = manifest.ID
type Version = manifest.Version
type Requirement = manifest.Requirement
type Dependency = manifest.Dependency
type Manifest = manifest.Manifest
type Plugin = foundation.Plugin
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

func (m Module) Manifest() Manifest { return m.Declaration.Snapshot() }
func (m Module) Register(r *Registrar) error {
	if m.OnRegister != nil {
		return m.OnRegister(r)
	}
	return nil
}
func (m Module) Boot(ctx context.Context, r *foundation.Runtime) error {
	if m.OnBoot != nil {
		return m.OnBoot(ctx, r)
	}
	return nil
}
func (m Module) Shutdown(ctx context.Context, r *foundation.Runtime) error {
	if m.OnShutdown != nil {
		return m.OnShutdown(ctx, r)
	}
	return nil
}

func Provider(id ID) foundation.ProviderID { return foundation.PluginProvider(id) }
