package invalid

import (
	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
)

func invalid(group http.GuardBinding[bootstrap.Member], operator auth.Guard[bootstrap.Operator]) {
	_, _ = group.Select(operator)
}
