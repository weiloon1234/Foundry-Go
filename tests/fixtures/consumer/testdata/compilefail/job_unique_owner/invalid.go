package invalid

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

var foreign jobs.UniqueKey[background.RemoveExport]
var _ = jobs.Unique[background.Welcome]{Key: foreign}
