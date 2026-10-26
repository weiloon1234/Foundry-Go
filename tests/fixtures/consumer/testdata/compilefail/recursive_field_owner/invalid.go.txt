package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type parentAlias struct{}

func invalid(self query.RecursiveSelf[models.User]) {
	parent := query.As[parentAlias](self, "parent")
	_ = models.OrderFieldsAt(parent.Scope())
}
