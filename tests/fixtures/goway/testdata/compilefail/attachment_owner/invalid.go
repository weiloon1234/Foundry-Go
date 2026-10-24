package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(manager *attachments.Manager) {
	collection := attachments.Define(a.Owner, "files", attachments.Policy{})
	_, _ = collection.Replace(context.Background(), manager, orm.IdentityOf(a.Authors, a.Authors.Key()).Reference(1), attachments.Upload{})
}
