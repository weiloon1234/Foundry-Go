package profiles

import (
	"image/color"

	"github.com/weiloon1234/Foundry-Go/imaging"
)

// ProfileLabelPlan keeps application code to domain content and appearance.
// Load BuiltinFont or ParseFont once during assembly and reuse the owned font.
func ProfileLabelPlan(label string, font imaging.Font) imaging.Plan {
	return imaging.NewPlan().
		Contain(320, 160, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
		Rectangle(8, 100, 304, 52, imaging.ShapeStyle{Fill: color.NRGBA{R: 20, G: 40, B: 70, A: 255}}).
		Text(label, imaging.TextOptions{
			Font: font, Size: 24, Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			Position: imaging.Bottom, Y: -20, MaxWidth: 280, Align: imaging.AlignCenter,
		}).Format(imaging.PNG)
}
