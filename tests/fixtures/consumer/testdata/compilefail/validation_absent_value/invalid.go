package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = validation.Check(validation.Absent[string](), context.Background(), value.Set(1), validation.DefaultLimits())
