package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type Code string

func (Code) MarshalText() ([]byte, error) { return nil, nil }
func (*Code) UnmarshalText([]byte) error  { return nil }

var _ = foundryhttp.TextPath[Code, Code]()
