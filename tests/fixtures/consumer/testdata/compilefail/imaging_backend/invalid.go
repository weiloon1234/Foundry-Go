package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(format imaging.Format) {
	config := imaging.DefaultConfig()
	config.Backend = format
}
