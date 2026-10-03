package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(filter imaging.Resampling) {
	_ = imaging.NewPlan().Format(imaging.PNG).PNGCompression(filter)
}
