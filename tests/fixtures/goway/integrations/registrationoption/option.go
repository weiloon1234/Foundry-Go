// Package registrationoption measures table registration without application construction.
package registrationoption

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/application"
)

func Option() application.Option {
	return application.WithTables(models.Tables()...)
}
