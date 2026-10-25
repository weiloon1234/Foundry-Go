package invalid

import "github.com/weiloon1234/Foundry-Go/database/lifecycle"

type model struct{}
type hooks struct{}

func invalid(set lifecycle.Observers) {
	counts := lifecycle.CountObservers[model](set)
	_, _ = lifecycle.ObserverFactories[model, hooks](counts)
}
