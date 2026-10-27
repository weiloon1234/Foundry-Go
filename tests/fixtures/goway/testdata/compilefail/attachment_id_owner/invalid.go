package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

func invalid(manager *attachments.Manager, id attachments.ID[a.Author]) {
	collection := attachments.Define(a.Owner, "files", attachments.Policy{})
	_, _ = collection.Find(context.Background(), manager, extensions.ORMIdentity(a.Owner).Reference(1), id)
}
