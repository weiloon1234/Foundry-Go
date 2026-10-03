package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(bytes []byte) {
	_ = imaging.TextOptions{Font: bytes}
}
