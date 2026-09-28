package invalid

import (
	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

func invalid(key string) { _ = message.Define(key, localization.WelcomeArgsJSON(), message.Options{}) }
