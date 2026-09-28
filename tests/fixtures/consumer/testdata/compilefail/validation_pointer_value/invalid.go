package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var text string
var _ = validation.Pointer(validation.Min(1)).Check(context.Background(), &text, validation.DefaultLimits())
