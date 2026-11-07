// Package realtime demonstrates typed WebSocket declarations and domain services
// without owning socket readers, writers, credential parsing or subscription maps.
package realtime

import (
	"context"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type OrdersOwner struct{}
type InboxOwner struct{}

// doc-example:begin channel-events
var Orders = websocket.Public[OrdersOwner]("orders", websocket.DefineRooms(foundryhttp.ModelIDPath[models.Order]())).WithReplay(websocket.ReplayConfig{Messages: 8, Bytes: 32 << 10, TTL: time.Minute})
var OrderUpdated = websocket.DefineOutgoing(Orders, "updated", httpdto.OrderResponseJSON())
var Inspect = websocket.DefineIncoming(Orders, "inspect", httpdto.OrderResponseJSON())
var Relay = websocket.DefineIncoming(Orders, "relay", httpdto.OrderResponseJSON()).AcknowledgeAccepted()

// doc-example:end channel-events
var HubKey = foundation.NewKey[*websocket.Hub]("realtime.hub")

type OrderService interface {
	Inspect(context.Context, model.ID[models.Order], httpdto.OrderResponse) error
}

// doc-example:begin register-channel
func Registry(service OrderService) (*websocket.Registry, error) {
	incoming := Inspect.Authorize(authorizeOrder).Handle(func(ctx context.Context, message websocket.MessageContext[model.ID[models.Order], websocket.Anonymous], input httpdto.OrderResponse) error {
		room, _ := message.Target.Room.Get()
		return service.Inspect(ctx, room, input)
	})
	return websocket.NewRegistry(websocket.Register(Orders, incoming, OrderUpdated.Registration(), Relay.Authorize(authorizeOrder).Relay(OrderUpdated)))
}

// doc-example:end register-channel
// doc-example:begin publish-room
func PublishOrder(ctx context.Context, hub websocket.PublisherSource, room model.ID[models.Order], reply httpdto.OrderResponse) (websocket.MessageID, error) {
	return websocket.Publish(ctx, hub, Orders, room, OrderUpdated, reply)
}

// doc-example:end publish-room
func Module(service OrderService, config websocket.Config, server websocket.ServerConfig) foundation.Module {
	return websocket.Module("realtime", HubKey, server, nil, func(foundation.Resolver) (*websocket.Hub, error) { return NewHub(service, config) })
}

// UserInbox is a separate authenticated channel with a different nominal owner.
// The helper checks the guard and stored key; IDs alone never grant access.
// doc-example:begin owned-rooms
func UserInbox(guard auth.Guard[models.User]) websocket.Channel[InboxOwner, model.ID[models.User], models.User] {
	return websocket.OwnedRooms[InboxOwner]("users.inbox", websocket.DefineRooms(foundryhttp.ModelIDPath[models.User]()), guard, orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf((models.User{})))
}

// doc-example:end owned-rooms

// NewHub and NewDistributedHub share the exact declarations and domain binding.
func NewHub(service OrderService, config websocket.Config) (*websocket.Hub, error) {
	r, err := Registry(service)
	if err != nil {
		return nil, err
	}
	return websocket.New(r, nil, config)
}

// doc-example:begin distributed-hub
func NewDistributedHub(service OrderService, config websocket.Config, backend websocket.ClusterBackend, cluster websocket.ClusterConfig) (*websocket.Hub, error) {
	r, err := Registry(service)
	if err != nil {
		return nil, err
	}
	return websocket.NewDistributed(r, nil, config, backend, cluster)
}

// doc-example:end distributed-hub

// A job/HTTP publisher borrows Redis but owns no listener or authentication scope.
// doc-example:begin standalone-publisher
func NewPublisher(config websocket.Config, backend websocket.ClusterBackend, cluster websocket.ClusterConfig) (*websocket.Publisher, error) {
	r, err := Registry(nil)
	if err != nil {
		return nil, err
	}
	return websocket.NewPublisher(r, config, backend, cluster)
}

// doc-example:end standalone-publisher
func RevokeUser(ctx context.Context, source websocket.PublisherSource, guard auth.Guard[models.User], user models.User) error {
	return websocket.DisconnectSubject(ctx, source, guard, orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user))
}
func Diagnostics(ctx context.Context, hub *websocket.Hub, guard auth.Guard[models.User], authorize func(context.Context, models.User) error) (websocket.Diagnostics, error) {
	return websocket.Diagnose(ctx, hub, guard, authorize)
}

func authorizeOrder(_ context.Context, message websocket.MessageContext[model.ID[models.Order], websocket.Anonymous], input httpdto.OrderResponse) error {
	room, present := message.Target.Room.Get()
	if !present || room != input.ID {
		return auth.Forbidden
	}
	return nil
}
