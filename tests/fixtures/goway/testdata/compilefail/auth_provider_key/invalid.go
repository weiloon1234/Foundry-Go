package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() {
	_ = auth.DefineProvider("members", a.MemberIdentity.ReferenceOf(a.Member{}), func(context.Context, model.ID[a.Other]) (value.Optional[a.Member], error) {
		return value.Optional[a.Member]{}, nil
	}, func(context.Context, a.Member) (bool, error) { return true, nil })
}
