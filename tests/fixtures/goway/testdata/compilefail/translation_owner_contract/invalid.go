package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager) {
	field := translations.Define(a.Owner, "title", translations.Options{})
	_ = field.Set(context.Background(), manager, orm.IdentityOf(a.Authors, a.Authors.Key()).Reference(1), "en", "text")
}
