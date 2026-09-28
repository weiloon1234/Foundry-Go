package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad() { _, _ = auth.NewProof((models.User{}).FoundryReference(), "authenticated") }
