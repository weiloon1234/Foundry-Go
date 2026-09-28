package compilefail

import "github.com/weiloon1234/Foundry-Go/foundation"

var _ = foundation.Contribute(nil, foundation.NewCollection[[]byte]("bytes"), "entry", func(foundation.Resolver) (string, error) { return "wrong", nil })
