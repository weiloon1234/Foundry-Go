package hostcapabilities

import (
	"context"
	"errors"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/deployment"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// doc-example:begin worker-client-contract
type workerClient interface{ Run(context.Context) error }

// doc-example:end worker-client-contract

func rendererWorkers(bindings deployment.Bindings, NewWorkerClient func(int, deployment.Endpoint, string) workerClient) ([]workerClient, error) {
	// doc-example:begin host-capabilities-2
	var workers []workerClient
	copies, err := bindings.Copies("renderers")
	if err != nil {
		return nil, err
	}
	for _, copy := range copies {
		// Pass these concrete values to the application-owned worker client.
		client := NewWorkerClient(copy.Ordinal, copy.Endpoint, copy.SecretFile)
		workers = append(workers, client)
	}
	// doc-example:end host-capabilities-2
	return workers, nil
}

// doc-example:begin media-preparation-cache
func mediaPreparationCache() (deployment.SharedDirectory, bool, error) {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return deployment.SharedDirectory{}, false, err
	}
	cache, err := bindings.SharedDirectory("render-cache")
	if errors.Is(err, fault.Missing) {
		// HTTP, worker, and console processes have no grant; assembly continues.
		return deployment.SharedDirectory{}, false, nil
	}
	if err != nil {
		return deployment.SharedDirectory{}, false, err
	}
	if cache.Access != deployment.DirectoryReadWrite {
		return deployment.SharedDirectory{}, false, fmt.Errorf("media preparation requires write access")
	}
	return cache, true, nil
}

// doc-example:end media-preparation-cache
