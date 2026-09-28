package invalid

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Before[temporal.Date](temporal.DateTime{})
