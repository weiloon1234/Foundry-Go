package invalid

import "github.com/weiloon1234/Foundry-Go/foundation"

func register(r *foundation.Registrar) error {
	return foundation.Provide(r, foundation.NewKey[int]("count"), "wrong")
}
