package invalid

import (
	"context"
	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type Other struct {
	Name string
	Age  uint16
}

var _ = validation.Check(validationinput.Rules, context.Background(), Other{}, validation.DefaultLimits())
