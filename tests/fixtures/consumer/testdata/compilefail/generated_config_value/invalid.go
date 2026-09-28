package invalid

import "foundry.test/consumer/startupconfig"

var _ = startupconfig.SettingsConfigKeys().HTTP.Port.Set("9000")
