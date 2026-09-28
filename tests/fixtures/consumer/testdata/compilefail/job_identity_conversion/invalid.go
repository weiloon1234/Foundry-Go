package compilefail

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid() {
	_ = jobs.ID[background.Welcome](jobs.ID[background.RemoveExport]{})
}
