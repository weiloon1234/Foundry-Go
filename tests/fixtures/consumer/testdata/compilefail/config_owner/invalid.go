package invalid

import "github.com/weiloon1234/Foundry-Go/config"

type first struct{ Port int }
type second struct{ Port int }

var port = config.Int("http.port", func(s *first) *int { return &s.Port })
var _, _ = config.New[second](port)
