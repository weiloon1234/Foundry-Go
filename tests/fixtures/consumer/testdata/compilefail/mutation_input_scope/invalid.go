package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}

var source = query.As[alias](inputqueries.QueryInputMembers(), "other")
var fields = inputqueries.MemberFieldsAt(source.Scope())
var _ = query.OnConflict(inputqueries.MemberFields().ID).DoUpdate(fields.Email.Set(inputqueries.EmailInput{}))
