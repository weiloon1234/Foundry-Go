package foundation_test

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

func pluginModule(id plugin.ID, dependencies ...plugin.Dependency) plugin.Module {
	return plugin.Module{Declaration: plugin.Manifest{ID: id, Version: "1.0.0", Framework: "^0.1.0", Dependencies: dependencies}}
}

func TestPluginGraphFailsBeforeRegistration(t *testing.T) {
	for _, test := range []struct {
		name    string
		plugins []plugin.Module
		kind    fault.Code
	}{
		{"missing", []plugin.Module{pluginModule("child", plugin.Dependency{ID: "base", Version: "^1.0.0"})}, fault.Missing},
		{"version", []plugin.Module{pluginModule("child", plugin.Dependency{ID: "base", Version: "^2.0.0"}), pluginModule("base")}, fault.Conflict},
		{"cycle", []plugin.Module{pluginModule("child", plugin.Dependency{ID: "base", Version: "*"}), pluginModule("base", plugin.Dependency{ID: "child", Version: "*"})}, fault.Cycle},
		{"duplicate", []plugin.Module{pluginModule("base"), pluginModule("base")}, fault.Duplicate},
		{"self", []plugin.Module{pluginModule("base", plugin.Dependency{ID: "base", Version: "*"})}, fault.Cycle},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			builder := foundation.NewBuilder()
			for _, extension := range test.plugins {
				extension.OnRegister = func(*plugin.Registrar) error { calls.Add(1); return nil }
				builder.RegisterPlugin(extension)
			}
			_, err := builder.Build(t.Context())
			if !errors.Is(err, test.kind) || calls.Load() != 0 {
				t.Fatalf("pre-registration failure: %v; calls=%d", err, calls.Load())
			}
		})
	}
	bad := pluginModule("incompatible")
	bad.Declaration.Framework = ">=9.0.0"
	if _, err := foundation.NewBuilder().RegisterPlugin(bad).Build(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if _, err := foundation.NewBuilder().Register(foundation.Module{Name: plugin.Provider("spoof")}).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("provider impersonated plugin", err)
	}
}

func TestPluginInspectionDoesNotConstructOrBoot(t *testing.T) {
	var constructions, boots atomic.Int32
	key := foundation.NewKey[string]("plugin.message")
	base := pluginModule("base")
	base.OnRegister = func(r *plugin.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (string, error) { constructions.Add(1); return "ready", nil })
	}
	base.OnBoot = func(context.Context, *foundation.Runtime) error { boots.Add(1); return nil }
	child := pluginModule("child", plugin.Dependency{ID: "base", Version: "^1.0.0"})
	builder := foundation.NewBuilder().RegisterPlugin(child, base)
	// Mutation after registration cannot change the captured dependency graph.
	child.Declaration.Dependencies[0].ID = "missing"
	inspection, err := builder.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if constructions.Load() != 0 || boots.Load() != 0 || len(inspection.Plugins) != 2 || inspection.Plugins[0].ID != "base" {
		t.Fatalf("inspection invoked runtime or reordered dependencies: %+v", inspection)
	}
	inspection.Plugins[1].Dependencies[0].ID = "corrupt"
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if constructions.Load() != 1 || boots.Load() != 0 {
		t.Fatal("Build lifecycle changed")
	}
	snapshot := app.Inspect()
	if snapshot.Plugins[1].Dependencies[0].ID != "base" {
		t.Fatal("inspection shares immutable graph")
	}
	snapshot.Contributions[0].Owner = "corrupt"
	if app.Inspect().Contributions[0].Owner != plugin.Provider("base") {
		t.Fatal("app inspection shares contribution storage")
	}
}

func TestPluginPartialBootAndShutdownOrder(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial"}[fail], func(t *testing.T) {
			log := &journal{}
			base := pluginModule("base")
			base.OnBoot = func(_ context.Context, r *foundation.Runtime) error {
				log.add("base boot")
				return r.OnShutdown("connection", func(context.Context) error { log.add("base resource"); return nil })
			}
			base.OnShutdown = func(context.Context, *foundation.Runtime) error { log.add("base shutdown"); return nil }
			child := pluginModule("child", plugin.Dependency{ID: "base", Version: "*"})
			failure := errors.New("boot refused")
			child.OnBoot = func(_ context.Context, r *foundation.Runtime) error {
				log.add("child boot")
				if err := r.OnShutdown("partial", func(context.Context) error { log.add("child resource"); return nil }); err != nil {
					return err
				}
				if fail {
					return failure
				}
				return nil
			}
			child.OnShutdown = func(context.Context, *foundation.Runtime) error { log.add("child shutdown"); return nil }
			app, err := foundation.NewBuilder().RegisterPlugin(child, base).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = app.Start(t.Context())
			if fail && !errors.Is(err, failure) || !fail && err != nil {
				t.Fatalf("startup: %v", err)
			}
			_ = stop(t, app)
			want := []string{"base boot", "child boot", "child shutdown", "child resource", "base shutdown", "base resource"}
			if fail {
				want = []string{"base boot", "child boot", "child resource", "base shutdown", "base resource"}
			}
			if !slices.Equal(log.all(), want) {
				t.Fatalf("cleanup order: %v", log.all())
			}
		})
	}
}

func TestPluginContributionDuplicatesAndExplicitOverrides(t *testing.T) {
	key := foundation.NewKey[string]("reports.title")
	base := pluginModule("base")
	base.OnRegister = func(r *plugin.Registrar) error { return foundation.Provide(r, key, "default") }
	child := pluginModule("child")
	child.OnRegister = func(r *plugin.Registrar) error { return foundation.Provide(r, key, "second") }
	_, err := foundation.NewBuilder().RegisterPlugin(base, child).Build(t.Context())
	if !errors.Is(err, fault.Duplicate) || !strings.Contains(err.Error(), "plugin:base") || !strings.Contains(err.Error(), "plugin:child") {
		t.Fatalf("duplicate owners absent: %v", err)
	}
	app, err := foundation.NewBuilder().RegisterPlugin(base).OverrideContributions("application", func(r *foundation.Registrar) error { return foundation.Provide(r, key, "custom") }).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := foundation.Resolve(app.Services(), key); err != nil || got != "custom" {
		t.Fatalf("override: %s %v", got, err)
	}
	if info := app.Inspect().Contributions[0]; info.Owner != "application" || info.Replaces != plugin.Provider("base") {
		t.Fatalf("ownership lost: %+v", info)
	}
	for _, register := range []func(*foundation.Registrar) error{
		func(r *foundation.Registrar) error {
			return foundation.Provide(r, foundation.NewKey[int](key.Name()), 3)
		},
		func(r *foundation.Registrar) error {
			return foundation.Provide(r, foundation.NewKey[string]("missing"), "value")
		},
		func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, key, "one"); err != nil {
				return err
			}
			return foundation.Provide(r, key, "two")
		},
	} {
		if _, err := foundation.NewBuilder().RegisterPlugin(base).OverrideContributions("application", register).Build(t.Context()); err == nil {
			t.Fatal("invalid override accepted")
		}
	}
}

type hostileManifestPlugin struct {
	plugin.Module
	exit bool
}

func (p hostileManifestPlugin) Manifest() plugin.Manifest {
	if p.exit {
		runtime.Goexit()
	}
	panic("private payload")
}

func TestPluginCallbackFailuresAreContained(t *testing.T) {
	for _, exit := range []bool{false, true} {
		_, err := foundation.NewBuilder().RegisterPlugin(hostileManifestPlugin{exit: exit}).Build(t.Context())
		if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private payload") {
			t.Fatalf("manifest callback containment: %v", err)
		}
		module := pluginModule("callback")
		module.OnRegister = func(*plugin.Registrar) error {
			if exit {
				runtime.Goexit()
			}
			panic("private payload")
		}
		_, err = foundation.NewBuilder().RegisterPlugin(module).Build(t.Context())
		if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private payload") {
			t.Fatalf("register callback containment: %v", err)
		}
	}
}

func TestPluginShutdownFailuresStillReleaseResources(t *testing.T) {
	for _, exit := range []bool{false, true} {
		log := &journal{}
		module := pluginModule("callback")
		module.OnBoot = func(_ context.Context, r *foundation.Runtime) error {
			return r.OnShutdown("acquired", func(context.Context) error { log.add("resource"); return nil })
		}
		module.OnShutdown = func(context.Context, *foundation.Runtime) error {
			if exit {
				runtime.Goexit()
			}
			panic("private shutdown payload")
		}
		app, err := foundation.NewBuilder().RegisterPlugin(module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		err = stop(t, app)
		if !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private shutdown payload") || !slices.Equal(log.all(), []string{"resource"}) {
			t.Fatalf("shutdown containment: %v %v", err, log.all())
		}
	}
}

func TestPluginConfigurationNamespacesCannotOverlap(t *testing.T) {
	base := pluginModule("base")
	base.Declaration.ConfigNamespace = "plugins.reports"
	child := pluginModule("child")
	child.Declaration.ConfigNamespace = "plugins.reports.child"
	_, err := foundation.NewBuilder().RegisterPlugin(base, child).Inspect(t.Context())
	if !errors.Is(err, fault.Duplicate) || !strings.Contains(err.Error(), "base and child") {
		t.Fatalf("overlapping namespace accepted: %v", err)
	}
}
