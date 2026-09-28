package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.ByField[n.Path, n.Project, n.Slug](nil, n.QueryProjects(), n.ProjectFields().Slug, func(n.Path) int64 { return 1 })
