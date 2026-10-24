package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, raw string) {
	field := translations.Define(a.Owner, "title", translations.Options{})
	_ = field.Set(context.Background(), manager, a.Owner.Reference(1), raw, "text")
}
