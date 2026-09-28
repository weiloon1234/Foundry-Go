package jobs

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"slices"
)

// Module preserves the advanced single-connection API. Named assembly uses
// ConnectionModule for each dispatcher and exactly one selected WorkerModule.
func Module(name foundation.ProviderID, key foundation.Key[*Dispatcher], dispatch DispatchConfig, worker WorkerConfig, requires []foundation.ProviderID, construct func(foundation.Resolver) (Backend, []Declaration, error)) foundation.Module {
	worker = worker.snapshot()
	module := ConnectionModule(name, key, dispatch, requires, construct)
	register := module.OnRegister
	module.OnRegister = func(r *foundation.Registrar) error {
		if err := worker.Validate(); err != nil {
			return err
		}
		if dispatch.Namespace != worker.Namespace {
			return fault.New(fault.Invalid, "job module dispatcher and worker must share a namespace")
		}
		if err := register(r); err != nil {
			return err
		}
		return registerWorker(r, key, worker)
	}
	return module
}

// ConnectionModule freezes the typed registry and contributions without selecting
// a kernel. Backend ownership stays with requires; all kernels may dispatch.
func ConnectionModule(name foundation.ProviderID, key foundation.Key[*Dispatcher], dispatch DispatchConfig, requires []foundation.ProviderID, construct func(foundation.Resolver) (Backend, []Declaration, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "job module requires a backend and declaration factory")
		}
		if err := dispatch.Validate(); err != nil {
			return err
		}
		if err := foundation.Factory(r, key, func(resolver foundation.Resolver) (*Dispatcher, error) {
			backend, declarations, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			registry, err := NewRegistry(declarations...)
			if err != nil {
				return nil, err
			}
			dispatcher, err := NewDispatcher(backend, registry, dispatch)
			if err != nil {
				return nil, err
			}
			dispatcher.managed = true
			return dispatcher, nil
		}); err != nil {
			return err
		}
		return bindContributions(r, key)
	}}
}

// WorkerModule selects one dispatcher for the application's Worker kernel. Its
// Requires must include the selected connection and every handler dependency.
func WorkerModule(name foundation.ProviderID, key foundation.Key[*Dispatcher], config WorkerConfig, requires []foundation.ProviderID) foundation.Module {
	config = config.snapshot()
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if err := config.Validate(); err != nil {
			return err
		}
		// Construction validation runs even when a different kernel is selected.
		if err := foundation.Factory(r, foundation.NewKey[workerBinding]("foundry.jobs.worker."+string(name)), func(resolver foundation.Resolver) (workerBinding, error) {
			dispatcher, err := foundation.Resolve(resolver, key)
			if err != nil {
				return workerBinding{}, err
			}
			return workerBinding{}, validateWorkerDispatcher(dispatcher, config)
		}); err != nil {
			return err
		}
		return registerWorker(r, key, config)
	}}
}

type workerBinding struct{}

func validateWorkerDispatcher(dispatcher *Dispatcher, config WorkerConfig) error {
	if dispatcher == nil || dispatcher.slots == nil || dispatcher.config.Namespace != config.Namespace {
		return fault.New(fault.Invalid, "worker and dispatcher must share a namespace")
	}
	return nil
}
func registerWorker(r *foundation.Registrar, key foundation.Key[*Dispatcher], config WorkerConfig) error {
	return r.Kernel(foundation.Worker, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
		dispatcher, err := foundation.Resolve(runtime.Services(), key)
		if err != nil {
			return nil, err
		}
		if err := validateWorkerDispatcher(dispatcher, config); err != nil {
			return nil, err
		}
		return NewWorker(dispatcher.backend, dispatcher.registry, config)
	})
}
