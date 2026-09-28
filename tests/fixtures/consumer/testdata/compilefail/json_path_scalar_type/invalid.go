package invalid

import (
	"foundry.test/consumer/jsonqueries"
)

func invalid() {
	_ = jsonqueries.DocumentFields().Settings.Properties().Theme.Scalar().Eq(42)
}
