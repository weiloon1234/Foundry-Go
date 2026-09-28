package httpquery

import "github.com/weiloon1234/Foundry-Go/contract"

// JSONContract lets generated DTOs discover TrackingCode's declared wire shape.
// TrackingScalar already owns the URL metadata; native methods own validation.
func (TrackingCode) JSONContract() contract.JSON[TrackingCode] {
	return contract.ScalarJSON(TrackingScalar)
}
