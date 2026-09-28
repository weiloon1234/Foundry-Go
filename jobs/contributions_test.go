package jobs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

func TestPluginJobCanResolveItsOwnDispatcher(t *testing.T) {
	key := foundation.NewKey[*jobs.Dispatcher]("plugin.jobs")
	definition := jobs.Define[payload]("plugin.reports", 1, jobs.DefaultPolicy("default"))
	namespace := keyspace.Namespace{Application: "plugin", Environment: "test"}
	module := jobs.Module("jobs", key, jobs.DefaultDispatchConfig(namespace), jobs.DefaultWorkerConfig(namespace, "default"), nil, func(foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		backend, err := memory.New(memory.DefaultConfig())
		return backend, nil, err
	})
	var captured *jobs.Dispatcher
	register := func(r *foundation.Registrar) error {
		return jobs.RegisterJob(r, key, definition, func(resolver foundation.Resolver) (jobs.Declaration, error) {
			var err error
			captured, err = foundation.Resolve(resolver, key)
			if err != nil {
				return jobs.Declaration{}, err
			}
			return definition.Declare(func(context.Context, payload) error { return nil })
		})
	}
	extension := plugin.Module{Declaration: plugin.Manifest{ID: "reports", Version: "1.0.0", Framework: "*"}, OnRegister: register}
	app, err := foundation.NewBuilder().Register(module).RegisterPlugin(extension).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := foundation.Resolve(app.Services(), key)
	if err != nil || captured != dispatcher {
		t.Fatalf("self-dispatch construction: %v", err)
	}
	if _, err := definition.Dispatch(t.Context(), dispatcher, payload{}, jobs.Options[payload]{}); err != nil {
		t.Fatal("contributed job missing from bound registry", err)
	}
	duplicate := extension
	duplicate.Declaration.ID = "duplicate"
	_, err = foundation.NewBuilder().Register(module).RegisterPlugin(extension, duplicate).Build(t.Context())
	if !errors.Is(err, fault.Duplicate) || !strings.Contains(err.Error(), "plugin:reports") || !strings.Contains(err.Error(), "plugin:duplicate") {
		t.Fatalf("duplicate job owners: %v", err)
	}
	_, err = foundation.NewBuilder().RegisterPlugin(extension).Build(t.Context())
	if !errors.Is(err, fault.Missing) {
		t.Fatal("orphan job contribution accepted", err)
	}
}
