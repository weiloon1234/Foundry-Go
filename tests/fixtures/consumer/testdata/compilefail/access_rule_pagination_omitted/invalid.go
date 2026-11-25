package invalid

import (
	"foundry.test/consumer/httppagination"
	http "github.com/weiloon1234/Foundry-Go/http"
	pagination "github.com/weiloon1234/Foundry-Go/http/pagination"
)

var binding http.GuardBinding[httppagination.Actor]
var _ = pagination.Authenticated(httppagination.GuardedList, binding)
