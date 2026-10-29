package invalid

import (
	"context"
	"foundry.test/goway/coremodels"
	h "foundry.test/goway/models/hooks"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = context.Background
var _ *database.DB
var _ foundation.Resolver
var _ value.Optional[h.Member]
var _ m.Member
var _ orm.Hooks[h.Member]

var _ value.Optional[value.Nullable[coremodels.StoredLabel]] = orm.WriteNullableGet(new(orm.Write[m.Member]), func() func(arg0 coremodels.LabelInput) orm.Assignment[m.Member] {
	receiver := m.Members.Note
	return func(arg0 coremodels.LabelInput) orm.Assignment[m.Member] {
		return orm.NullableInputTextSet(receiver, arg0)
	}
}())
