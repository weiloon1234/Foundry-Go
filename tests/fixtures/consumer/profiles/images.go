package profiles

import (
	"image/color"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ProfileCardPlan is reusable as a direct processing plan or attachment variant.
// The prepared watermark owns its bytes and can be shared across requests.
func ProfileCardPlan(watermark imaging.Result) imaging.Plan {
	return imaging.NewPlan().
		Contain(1200, 630, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
		Insert(watermark, imaging.Placement{Position: imaging.BottomRight, X: -16, Y: -16, Opacity: value.Set(0.75)}).
		Format(imaging.PNG)
}
