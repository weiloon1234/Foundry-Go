package querycore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/encryption"
)

// sealer encrypts one captured plaintext for the row it is written to.
type Sealer func(context.Context, *encryption.Keyring, encryption.Context) (AssignmentValue, error)
