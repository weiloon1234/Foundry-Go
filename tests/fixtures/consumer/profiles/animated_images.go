package profiles

import "github.com/weiloon1234/Foundry-Go/imaging"

// AnimatedAvatarPlan keeps all frames and timing while sizing each canvas.
// PNG output is animated PNG when the input is animated, and PNG otherwise.
func AnimatedAvatarPlan() imaging.Plan {
	return imaging.NewPlan().
		Frames(imaging.PreserveAnimation).
		Fit(320, 320, false).
		Format(imaging.PNG)
}

// AnimatedWebPAvatarPlan shares the avatar geometry with lossless WebP output.
func AnimatedWebPAvatarPlan() imaging.Plan {
	return AnimatedAvatarPlan().Format(imaging.WebP)
}
