package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = validation.Check(validation.Optional(validation.Min[int](1)), context.Background(), value.Set("wrong"), validation.DefaultLimits())
