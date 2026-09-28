package invalid

import "github.com/weiloon1234/Foundry-Go/contract"

type First string
type Second string

var _ = contract.JSONType[First]("app.First", func() contract.JSON[Second] {
	return contract.JSON[Second]{}
})
