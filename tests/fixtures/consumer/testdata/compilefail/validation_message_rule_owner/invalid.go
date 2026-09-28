package invalid

import (
	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ validation.Rule[int] = validation.WithTranslation(validation.NonBlank[string](), localization.RuleArgsMessage(), localization.RuleArgs{})
