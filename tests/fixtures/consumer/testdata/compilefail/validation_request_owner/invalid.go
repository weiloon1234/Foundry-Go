package invalid

import (
	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type Other struct{ Name string }

var field = validation.DefineField("name", func(p Other) string { return p.Name })
var _ = validation.All[validationinput.Request](field.Rules(validation.NonBlank[string]()))
