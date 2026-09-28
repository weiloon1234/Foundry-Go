package invalid

import (
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = storage.PutOptions{Checksum: value.Set("not a typed checksum")}
