package invalid

import (
	"foundry.test/consumer/jsonqueries"
)

func invalid() {
	_ = jsonqueries.DocumentFields().Settings.Properties().Profile.Properties().Attributes.At("2")
}
