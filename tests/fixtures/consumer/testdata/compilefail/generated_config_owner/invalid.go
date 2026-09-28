package invalid

import (
	"foundry.test/consumer/startupconfig"
	"github.com/weiloon1234/Foundry-Go/config"
)

type Other struct{ Port startupconfig.Port }

var _ config.Override[Other] = startupconfig.SettingsConfigKeys().HTTP.Port.Set(9000)
