package invalid

import "github.com/weiloon1234/Foundry-Go/auth"

func bad(name auth.GuardName) { _ = auth.Credential{Name: name} }
