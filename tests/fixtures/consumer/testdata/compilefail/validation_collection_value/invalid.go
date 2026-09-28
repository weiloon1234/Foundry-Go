package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Distinct[[]int]().Check(context.Background(), []string{"one"}, validation.DefaultLimits())
