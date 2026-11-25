package httpstreams

import (
	"context"
	"io"
	stdhttp "net/http"
	"time"

	"foundry.test/consumer/models"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin report-stream-contracts
// UserPath selects the user whose report is exported.
//
//foundry:path pattern=/users/{user}
type UserPath struct{ User model.ID[models.User] }

// ExportRequest carries the decoded user identity to the stream handler.
type ExportRequest = foundryhttp.Input[UserPath, foundryhttp.NoQuery, foundryhttp.NoBody]

// ReportSource authorizes access before returning a context-aware reader.
type ReportSource interface {
	Open(context.Context, model.ID[models.User]) (io.ReadCloser, error)
}

// doc-example:end report-stream-contracts

func registerReportExport(reports ReportSource) foundryhttp.RouteRegistration {
	// doc-example:begin http-streams-1
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "reports.export", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, UserPathDescriptor())
	endpoint := foundryhttp.DefineEndpoint(
		route,
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
		foundryhttp.StreamResponse("text/csv; charset=utf-8"),
	)
	registration := endpoint.Handle(func(ctx context.Context, input ExportRequest) (foundryhttp.Stream, error) {
		return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
			body, err := reports.Open(ctx, input.Path.User)
			return foundryhttp.StreamContent{
				Body: body, Name: "report.csv", MediaType: "text/csv; charset=utf-8",
			}, err
		}), nil
	})
	// doc-example:end http-streams-1
	return registration
}

// doc-example:begin order-event-contracts
// OrderEvent carries one order transition to a subscriber.
//
//foundry:dto
type OrderEvent struct {
	Number string `json:"number"`
	State  string `json:"state"`
}
type orderUpdate struct {
	Cursor string
	Event  OrderEvent
}
type orderWatcher interface {
	// Watch closes its channel when ctx ends.
	Watch(context.Context, string) <-chan orderUpdate
}

// doc-example:end order-event-contracts

func registerRawEvents(orders orderWatcher) foundryhttp.RouteRegistration {
	// doc-example:begin http-streams-2
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "orders.events", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, foundryhttp.StaticPath("/orders/events"))
	route = foundryhttp.ConfigureRoute(route, foundryhttp.Timeout(time.Hour))

	registration := foundryhttp.HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ foundryhttp.NoPath) {
		err := foundryhttp.ServeEvents(w, r, OrderEventJSON(), foundryhttp.DefaultEventStreamConfig(),
			func(ctx context.Context, sink *foundryhttp.EventSink[OrderEvent]) error {
				for update := range orders.Watch(ctx, r.Header.Get("Last-Event-ID")) {
					if err := sink.Send(ctx, foundryhttp.Event[OrderEvent]{ID: update.Cursor, Name: "order", Data: update.Event}); err != nil {
						return err
					}
				}
				return nil
			})
		if err != nil {
			// The stream is committed; do not write a second response.
			return
		}
	})
	// doc-example:end http-streams-2
	return registration
}

func registerOrderEvents(orders orderWatcher) foundryhttp.RouteRegistration {
	// doc-example:begin http-streams-3
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "orders.events", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/orders/events"))

	orderEvents := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
		foundryhttp.EventStreamResponse(OrderEventJSON()))
	orderEvents = foundryhttp.ConfigureEndpoint(orderEvents, foundryhttp.Timeout(time.Hour))

	registration := orderEvents.Handle(func(ctx context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Events[OrderEvent], error) {
		return foundryhttp.EventsFrom(func(ctx context.Context, sink *foundryhttp.EventSink[OrderEvent]) error {
			after, _ := foundryhttp.LastEventID(ctx) // Set when the client resumes.
			for update := range orders.Watch(ctx, after) {
				if err := sink.Send(ctx, foundryhttp.Event[OrderEvent]{ID: update.Cursor, Data: update.Event}); err != nil {
					return err
				}
			}
			return nil
		}), nil
	})
	// doc-example:end http-streams-3
	return registration
}
