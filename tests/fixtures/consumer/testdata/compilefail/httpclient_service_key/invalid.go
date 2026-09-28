package invalid

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

var _ = httpclient.Module("client", foundation.NewKey[string]("client"), httpclient.DefaultConfig("client"), nil, nil)
