package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

var invalid = manifest.Sources{Realtime: &websocket.Config{}}
