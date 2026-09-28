package background

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

var DispatcherKey = foundation.NewKey[*jobs.Dispatcher]("background.dispatcher")

// WorkerModule borrows the caller's backend. Production assembly lists its
// owning Redis provider in requires so foundation preserves shutdown ordering.
func WorkerModule(sender WelcomeSender, backend jobs.Backend, namespace keyspace.Namespace, requires ...foundation.ProviderID) foundation.Module {
	return jobs.Module("background", DispatcherKey, jobs.DefaultDispatchConfig(namespace), jobs.DefaultWorkerConfig(namespace, "communications"), requires, func(foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		declaration, err := WelcomeDeclaration(sender)
		if err != nil {
			return nil, nil, err
		}
		return backend, []jobs.Declaration{declaration}, nil
	})
}

// WelcomeChain keeps concrete payload capture in application code. The framework
// owns atomic acceptance, progress, retry, cancellation and history.
func WelcomeChain(first, second jobs.Pending[Welcome]) (jobs.Workflow, error) {
	return jobs.NewChain(first.Step(), second.Step())
}
