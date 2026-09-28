package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/inspection"
)

func invalid(section string) { _ = inspection.Arguments{Section: section} }
