// Package notifying demonstrates typed notification declarations and thin
// application services. Foundry owns persistence, retries and recipient scoping.
package notifying

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/email"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

//foundry:dto
type OrderPlaced struct {
	OrderID model.ID[models.Order] `json:"order_id"`
	Summary string                 `json:"summary"`
}

//foundry:dto
type OrderCard struct {
	OrderID model.ID[models.Order] `json:"order_id"`
	Summary string                 `json:"summary"`
}
type UserRecipient = notifications.Recipient[models.User, model.ID[models.User]]
type OrdersBinding = notifications.Binding[models.User, model.ID[models.User], OrderPlaced]
type UserNotifications struct{}
type PrivateNotifications = notifications.Realtime[UserNotifications, models.User, model.ID[models.User], OrderCard]

var Placed = notifications.Define("order.placed", 1, OrderPlacedJSON())
var Delivery = notifications.DefineDeliveryJob("notifications.deliver", jobs.DefaultPolicy("communications"))
var Database = notifications.Database("inbox", OrderCardJSON(), card)

func Users(provider auth.Provider[models.User, model.ID[models.User]], guard auth.Guard[models.User]) UserRecipient {
	return notifications.DefineRecipient("users", provider, guard, func(context.Context, models.User, notifications.Name, notifications.ChannelID) (bool, error) {
		return true, nil
	})
}
func Private(recipient UserRecipient) PrivateNotifications {
	return notifications.DefineRealtime[UserNotifications](recipient, "users.notifications", websocket.DefineRooms(foundryhttp.ModelIDPath[models.User]()), "notification", OrderCardJSON())
}
func NewOrders(recipient UserRecipient, mailer *email.Mailer, from email.Address, realtime PrivateNotifications, publisher websocket.PublisherSource) OrdersBinding {
	mail := notifications.Email("email", mailer, func(_ context.Context, user models.User, _ notifications.DeliveryContext, input OrderPlaced) (email.Message, error) {
		to, err := email.ParseAddress(user.Email)
		return email.NewMessage(from, "Order placed", to).Text(input.Summary), err
	})
	real := notifications.RealtimeChannel("realtime", realtime, publisher, card)
	return notifications.Bind(Placed, recipient, Database.Channel(), mail, real)
}
func card(_ context.Context, _ models.User, _ notifications.DeliveryContext, input OrderPlaced) (OrderCard, error) {
	return OrderCard{input.OrderID, input.Summary}, nil
}

func Capture(ctx context.Context, binding OrdersBinding, user models.User, input OrderPlaced) (notifications.PendingNotification[models.User, OrderPlaced], error) {
	return binding.Capture(ctx, user.FoundryReference(), input, notifications.ID[models.User]{})
}
func Enqueue(ctx context.Context, tx *database.Tx, manager *notifications.Manager, producer *jobs.Outbox, pending notifications.PendingNotification[models.User, OrderPlaced]) (outbox.ID[notifications.DeliveryRequest], error) {
	return pending.Enqueue(ctx, tx, manager, Delivery, producer)
}
func Unread(ctx context.Context, inbox notifications.Inbox[models.User, model.ID[models.User]], page query.PageRequest) (query.Page[notifications.Record[models.User]], error) {
	return inbox.List(ctx, page, true)
}
