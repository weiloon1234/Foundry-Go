// Package applicationcontrol measures application construction without table registration.
package applicationcontrol

import "github.com/weiloon1234/Foundry-Go/application"

func Application(settings application.Settings) *application.Builder {
	return application.New(settings)
}
