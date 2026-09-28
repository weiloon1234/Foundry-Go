package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}

var _ = query.As[alias](models.QueryUsers().ForUpdate(), "locked")
