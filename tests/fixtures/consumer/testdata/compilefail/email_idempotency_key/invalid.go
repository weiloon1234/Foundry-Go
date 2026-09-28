package compilefail

import "github.com/weiloon1234/Foundry-Go/email"

func invalid(key string) { _ = email.SendOptions{IdempotencyKey: key} }
