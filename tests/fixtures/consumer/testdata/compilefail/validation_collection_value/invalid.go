package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Check(validation.Distinct[[]int](), context.Background(), []string{"one"}, validation.DefaultLimits())
