package sample

import "github.com/weiloon1234/Foundry-Go/value"

//foundry:query
type Input struct {
	Field value.Optional[value.Nullable[string]] `header:"X-Name"`
}
