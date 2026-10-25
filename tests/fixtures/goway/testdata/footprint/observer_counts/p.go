package observer_counts

import "github.com/weiloon1234/Foundry-Go/database/lifecycle"

type model struct{}

func Inspect(set lifecycle.Observers) lifecycle.RegistrationCounts {
	return lifecycle.CountObservers[model](set)
}
