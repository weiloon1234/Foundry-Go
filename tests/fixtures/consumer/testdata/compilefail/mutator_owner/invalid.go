package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid query.ModelField[models.User] = query.NewMutatedModelField("email_address", codec.String[string](), func(m mutatorqueries.Member) string { return m.Email }, (mutatorqueries.Member{}).MutateEmail)
