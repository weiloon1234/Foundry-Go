package invalid

import (
	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.WithTranslation(validation.NonBlank[string](), localization.RuleArgsMessage(), localization.WelcomeArgs{})
