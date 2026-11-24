package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var OrderModelRelations = struct{ Member orm.BelongsTo[Order, Member] }{orm.BelongsToOf(OrderModels.Member, OrderModels.MemberID, MemberModels.ID)}

var MemberModelRelations = struct{ Orders orm.HasMany[Member, Order] }{orm.HasManyOf(MemberModels.Orders, MemberModels.ID, OrderModels.MemberID)}

// OperatorIdentity preserves the stored report_operators identity and key codec.
var OperatorIdentity = orm.IdentityOf(OperatorModels.Table, OperatorModels.Key())
