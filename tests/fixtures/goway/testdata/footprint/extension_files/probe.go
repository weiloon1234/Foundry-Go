package probe

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

type Article struct{ ID int64 }

func Add(ctx context.Context, slot attachments.ManyHandle[Article, int64], owner Article, files []attachments.FileSource) ([]attachments.Result[Article, int64], error) {
	return attachments.AddFilesTo(ctx, slot, owner, files)
}
