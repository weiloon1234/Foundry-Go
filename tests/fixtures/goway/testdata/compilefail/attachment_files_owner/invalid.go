package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

type Article struct{ ID int64 }
type Other struct{ ID int64 }

func invalid(slot attachments.ManyHandle[Article, int64], files []attachments.FileSource) {
	_, _ = attachments.AddFilesTo(context.Background(), slot, Other{}, files)
}
