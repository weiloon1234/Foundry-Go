package invalid

import (
	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(guard application.BrowserGuard[bootstrap.Member, model.ID[bootstrap.Member]]) {
	var operator http.GuardBinding[bootstrap.Operator] = guard.Binding
	_ = operator
}
