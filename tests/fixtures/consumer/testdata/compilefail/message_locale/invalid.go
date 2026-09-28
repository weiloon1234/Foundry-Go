package invalid

import (
	"context"
	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func invalid(c *i18n.Catalog, locale string) {
	_, _ = localization.WelcomeArgsMessage().Format(context.Background(), c, locale, localization.WelcomeArgs{})
}
