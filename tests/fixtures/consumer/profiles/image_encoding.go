package profiles

import "github.com/weiloon1234/Foundry-Go/imaging"

// PNGDownloadPlan trades encoding time for smaller lossless exports.
func PNGDownloadPlan() imaging.Plan {
	return imaging.NewPlan().
		Format(imaging.PNG).
		PNGCompression(imaging.PNGBestCompression)
}

// AVIFPreviewPlan selects search effort and retains alpha independently of
// color compression. The portable encoder produces 8-bit 4:2:0 AVIF.
func AVIFPreviewPlan() imaging.Plan {
	return imaging.NewPlan().
		Format(imaging.AVIF).
		AVIFQuality(65).
		AVIFSpeed(6).
		AVIFAlphaQuality(100)
}

// WebPPreviewPlan compresses color while retaining lossless alpha.
func WebPPreviewPlan() imaging.Plan {
	return imaging.NewPlan().
		Format(imaging.WebP).
		WebPMode(imaging.WebPLossy).
		WebPQuality(80).
		WebPMethod(4)
}
