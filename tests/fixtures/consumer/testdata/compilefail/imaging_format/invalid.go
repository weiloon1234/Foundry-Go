package invalid

import (
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func invalid(media storage.MediaType) { _ = imaging.NewPlan().Format(media) }
