package invalid

import (
	"foundry.test/consumer/models"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func invalid(endpoint http.OptionalAuthenticationEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent], branch http.GuardAlternative[models.User]) {
	_ = http.ConfigureOptional[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent](endpoint, http.AnyOf(branch))
}
