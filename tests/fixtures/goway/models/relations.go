package models

import "github.com/weiloon1234/Foundry-Go/database/orm"

// OrderRelations is initialized after both table declarations.
var OrderRelations = struct{ Buyer orm.BelongsTo[Order, User] }{Buyer: orm.BelongsToOf(Orders.Buyer, Orders.BuyerID, Users.ID)}

// UserRelations keeps relation initialization out of model declaration cycles.
var UserRelations = struct {
	Orders     orm.HasMany[User, Order]
	Introducer orm.BelongsTo[User, User]
}{
	Orders:     orm.HasManyOf(Users.Orders, Users.ID, Orders.BuyerID),
	Introducer: orm.BelongsToOf(Users.Introducer, Users.IntroducerID, Users.ID),
}
