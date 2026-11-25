package httpquery

import "github.com/weiloon1234/Foundry-Go/value"

// doc-example:begin floating-url-types
type Latitude float32
type Distance float64
type Distances []Distance

//foundry:query
type NearbyInput struct {
	Latitude  Latitude
	Maximum   value.Optional[Distance]
	Distances Distances `query:"distance"`
}

//foundry:path pattern=/positions/{latitude}
type PositionPath struct{ Latitude Latitude }

// doc-example:end floating-url-types
