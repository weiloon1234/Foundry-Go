package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(mode imaging.Frames) {
	_ = imaging.NewPlan().Format(imaging.WebP).WebPMode(mode)
}
