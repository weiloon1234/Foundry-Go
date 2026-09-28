package invalid

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.NewMutatedModelField("email_address", codec.String[string](), func(m mutatorqueries.Member) string { return m.Email }, func(v int) (int, error) { return v, nil })
