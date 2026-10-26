package invalid

import (
	"foundry.test/consumer/jsonqueries"
)

func invalid() {
	_ = jsonqueries.QueryJsonPolicies().Where(jsonqueries.DocumentFields().Settings.Properties().Theme.Scalar().Eq("dark"))
}
