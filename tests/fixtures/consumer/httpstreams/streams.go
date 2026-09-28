// Package httpstreams verifies unseekable exports from an independent consumer.
package httpstreams

import (
	"context"
	"errors"
	"io"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Request = foundryhttp.Input[httpkernel.UserPath, foundryhttp.NoQuery, foundryhttp.NoBody]

var Export = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.export", Method: foundryhttp.GET, Access: foundryhttp.Public}, httpkernel.UserPathDescriptor()),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.StreamResponse("text/csv; charset=utf-8"),
)

// Reports checks domain access before returning the reader. Its reads honor ctx.
// The source owns cleanup until return, then Foundry closes the returned body.
type Reports interface {
	Open(context.Context, model.ID[models.User]) (io.ReadCloser, error)
}

func Router(reports Reports) (*foundryhttp.Router, error) {
	if reports == nil {
		return nil, errors.New("report router requires a service")
	}
	return foundryhttp.NewRouter(Export.Handle(func(ctx context.Context, input Request) (foundryhttp.Stream, error) {
		stream := foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
			body, err := reports.Open(ctx, input.Path.User)
			return foundryhttp.StreamContent{Body: body, Name: "report.csv", MediaType: "text/csv; charset=utf-8"}, err
		})
		return stream.WithName("report.csv"), nil
	}))
}
