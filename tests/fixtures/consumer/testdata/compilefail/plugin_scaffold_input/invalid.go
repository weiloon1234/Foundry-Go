package compilefail

import (
	"context"
	"foundry.test/plugindep"
	"github.com/weiloon1234/Foundry-Go/plugin/scaffold"
)

func invalid(value *scaffold.Scaffold[plugindep.ScaffoldInput]) {
	_, _ = value.Render(context.Background(), "wrong input")
}
