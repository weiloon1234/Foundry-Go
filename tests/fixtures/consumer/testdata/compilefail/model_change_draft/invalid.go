package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _, _ = mutatorqueries.CompareMember(value.Set(mutatorqueries.Member{}), value.Set(mutatorqueries.Member{}), models.UserDraft{})
