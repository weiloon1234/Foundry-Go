package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var fields = httpdto.UpdateUserValidationFields()
var comparator validation.Rule[validation.Pair[value.Optional[string]]]
var _ = validation.Compare(fields.Email, fields.Nickname, comparator)
