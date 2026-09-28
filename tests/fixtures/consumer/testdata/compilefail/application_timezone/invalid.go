package invalid

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

var _ = application.SettingsConfigKeys().TimeZone.Set(i18n.LocaleID("en"))
