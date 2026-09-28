package invalid

import "github.com/weiloon1234/Foundry-Go/contract"

var _ = contract.JSONMapType[map[int]string]("map", "string", "int", contract.StringJSONKey[string]())
