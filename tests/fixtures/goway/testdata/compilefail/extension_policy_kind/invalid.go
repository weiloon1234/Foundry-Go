package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

var _ = translations.TextOf(a.Owner, "title", attachments.Policy{}, extensions.BindSlot(a.Articles.Key(), a.Articles.Title))
