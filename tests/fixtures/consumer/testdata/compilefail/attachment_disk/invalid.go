package invalid

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(raw string) { _ = attachments.Policy{Disk: raw} }
