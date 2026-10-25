package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

type Article struct{ ID int64 }
type Other struct{ ID int64 }

func invalid(slot attachments.ManyHandle[Article, string], files []attachments.FileSource) {
	_, _ = attachments.AddFilesTo[attachments.FileSource, Article, int64](context.Background(), slot, Article{}, files)
}
