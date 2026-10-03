package invalid

import "github.com/weiloon1234/Foundry-Go/imaging"

func invalid(blend imaging.BlendMode) {
	_ = imaging.NewPlan().FillAt(100, 100, true, blend)
}
