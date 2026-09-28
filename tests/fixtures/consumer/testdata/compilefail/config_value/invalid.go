package invalid

import "github.com/weiloon1234/Foundry-Go/config"

type settings struct{ Port int }

var port = config.Int("http.port", func(s *settings) *int { return &s.Port })
var invalid = port.Set("wrong")
