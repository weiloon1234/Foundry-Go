package sample

import "github.com/weiloon1234/Foundry-Go/value"

//foundry:query
type Input struct {
	Field value.Optional[*string] `header:"X-Name"`
}
