package invalid

import (
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func invalid(media storage.MediaType) { _ = datatable.ExportOptions{Format: media} }
