package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var text string
var _ = validation.Check(validation.Pointer(validation.Min(1)), context.Background(), &text, validation.DefaultLimits())
