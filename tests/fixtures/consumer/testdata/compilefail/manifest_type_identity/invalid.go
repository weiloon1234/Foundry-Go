package invalid

import "github.com/weiloon1234/Foundry-Go/contract/manifest"

func invalid(identity string) { _ = manifest.Payload{Type: identity} }
