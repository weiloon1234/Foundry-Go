package invalid

import (
	"foundry.test/consumer/models"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func invalid(branch http.GuardAlternative[models.User]) {
	_ = http.AllOf[models.User](http.AnyOf(branch))
}
