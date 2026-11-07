package realtime

import (
	"context"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// doc-example:begin owned-channel
type Inbox struct {
	channel websocket.Channel[InboxOwner, model.ID[models.User], models.User]
	updated websocket.Outgoing[InboxOwner, httpdto.OrderResponse]
}

func NewInbox(guard auth.Guard[models.User]) Inbox {
	channel := UserInbox(guard)
	return Inbox{channel: channel, updated: websocket.DefineOutgoing(channel, "updated", httpdto.OrderResponseJSON())}
}

func (inbox Inbox) Registration() websocket.Registration {
	return websocket.Register(inbox.channel, inbox.updated.Registration())
}

// doc-example:end owned-channel

// doc-example:begin private-resource-channel
type PrivateOrdersOwner struct{}

func PrivateOrders(guard auth.Guard[models.User], authorize func(context.Context, models.User, model.ID[models.Order]) error) websocket.Channel[PrivateOrdersOwner, model.ID[models.Order], models.User] {
	return websocket.Private[PrivateOrdersOwner]("orders.private", websocket.DefineRooms(foundryhttp.ModelIDPath[models.Order]()), guard,
		func(ctx context.Context, actor models.User, target websocket.Target[model.ID[models.Order]]) error {
			room, present := target.Room.Get()
			if !present {
				return auth.Forbidden
			}
			return authorize(ctx, actor, room)
		})
}

// doc-example:end private-resource-channel

// doc-example:begin realtime-tickets
func SharedHubSettings(settings application.Settings, connection websocket.ConnectionName) application.Settings {
	settings.HTTP.Enabled = true
	settings.Realtime.Enabled = true
	settings.Realtime.Shared = true
	settings.Realtime.Publisher = false
	settings.Realtime.Connection = connection
	return settings
}

func ConfigureInbox(builder *application.Builder, inbox Inbox, authentication *foundryhttp.Authentication, tokens auth.TicketRedeemer) *application.Builder {
	return builder.Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{
			Channels:       []websocket.Registration{inbox.Registration()},
			Authentication: authentication,
			Tickets:        []auth.TicketRedeemer{tokens},
		}, nil
	})
}

// doc-example:end realtime-tickets

// doc-example:begin publish-after-commit
func (inbox Inbox) AfterCommit(tx *database.Tx, publisher websocket.PublisherSource, user model.ID[models.User], reply httpdto.OrderResponse) error {
	return tx.AfterCommit(func(ctx context.Context) error {
		_, err := websocket.Publish(ctx, publisher, inbox.channel, user, inbox.updated, reply)
		return err
	})
}

// doc-example:end publish-after-commit

// doc-example:begin configured-realtime-publisher
func PublisherSettings(settings application.Settings, connection websocket.ConnectionName) application.Settings {
	settings.HTTP.Enabled = false
	settings.Realtime.Enabled = false
	settings.Realtime.Shared = false
	settings.Realtime.Publisher = true
	settings.Realtime.Connection = connection
	return settings
}

type InboxPublisher struct {
	inbox     Inbox
	publisher websocket.PublisherSource
}

func NewInboxPublisher(services application.Services, inbox Inbox) (*InboxPublisher, error) {
	publisher, err := services.RealtimePublisher()
	if err != nil {
		return nil, err
	}
	return &InboxPublisher{inbox: inbox, publisher: publisher}, nil
}

func (p *InboxPublisher) Publish(ctx context.Context, user model.ID[models.User], reply httpdto.OrderResponse) (websocket.MessageID, error) {
	return websocket.Publish(ctx, p.publisher, p.inbox.channel, user, p.inbox.updated, reply)
}

// doc-example:end configured-realtime-publisher
