package invalid

import (
	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

var _ = message.Define[localization.WelcomeArgs]("welcome", localization.CartArgsJSON(), message.Options{})
