package invalid

import (
	"foundry.test/consumer/models"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func invalid() { _ = http.AllOf[models.User](http.ActorMiddleware[models.User]()) }
