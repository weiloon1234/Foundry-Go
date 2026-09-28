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

var _ = validationinput.Rules.Check(context.Background(), Other{}, validation.DefaultLimits())
