package invalid

import (
	"foundry.test/consumer/jsonqueries"
)

func invalid() {
	_ = jsonqueries.ProjectSnapshot(jsonqueries.QueryJsonDocuments()).SelectSettings(jsonqueries.DocumentFields().Tags.Value())
}
