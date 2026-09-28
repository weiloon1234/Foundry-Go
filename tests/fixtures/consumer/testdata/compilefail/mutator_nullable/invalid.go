package invalid

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = query.NewMutatedModelField("nickname", codec.String[string](), func(m mutatorqueries.Member) value.Nullable[string] { return m.Nickname }, (mutatorqueries.Member{}).MutateNickname)
