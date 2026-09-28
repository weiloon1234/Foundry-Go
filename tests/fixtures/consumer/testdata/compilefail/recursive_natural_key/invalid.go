package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/recursivequeries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type parentAlias struct{}

func invalid(self query.RecursiveSelf[recursivequeries.Node]) {
	parent := query.As[parentAlias](self, "parent")
	_ = recursivequeries.NodeFieldsAt(parent.Scope()).Code.Eq(models.User{}.ID)
}
