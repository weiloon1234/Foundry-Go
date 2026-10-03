package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(position imaging.Position) {
	_ = imaging.NewPlan().Frames(position)
}
