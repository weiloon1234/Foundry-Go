package registration

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/application"
)

// Application registers its handwritten models before providers can start.
func Application(settings application.Settings) *application.Builder {
	return application.New(settings, application.WithTables(models.Tables()...))
}
