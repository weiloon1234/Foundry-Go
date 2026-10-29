package invalid

import "github.com/weiloon1234/Foundry-Go/validation"

import "foundry.test/consumer/localization"

func invalid(key string) {
	_ = validation.WithLabelKey(localization.WelcomeArgsValidationFields().Name, key)
}
