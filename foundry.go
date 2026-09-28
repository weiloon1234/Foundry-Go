package foundry

import "github.com/weiloon1234/Foundry-Go/foundation"

// New starts direct foundation assembly. Register providers, Build the typed
// service graph, then Run one of the registered framework kernels. For configured
// services and HTTP assembly, use application.New from the application package.
func New(options ...foundation.Option) *foundation.Builder { return foundation.NewBuilder(options...) }
