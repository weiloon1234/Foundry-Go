package invalid

import (
	"foundry.test/consumer/jsonqueries"
)

func invalid() {
	_ = jsonqueries.DocumentFields().Tags.At("zero")
}
