// Package httpsigned verifies typed temporary links and domain handlers.
// It is a framework acceptance fixture, not an application scaffold.
package httpsigned

import (
	"context"
	"net/http"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type PreviewRequest = foundryhttp.Input[httpkernel.UserPath, httpquery.SearchInput, foundryhttp.NoBody]

var Preview = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "preview.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, httpkernel.UserPathDescriptor()),
	httpquery.SearchInputDescriptor(),
	foundryhttp.EmptyBody(),
	foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()),
)
var Asset = foundryhttp.DefineRoute(
	foundryhttp.RouteSpec{ID: "preview.asset", Method: foundryhttp.GET, Access: foundryhttp.Public},
	httpkernel.AssetPathDescriptor(),
)

type Links struct {
	Preview foundryhttp.SignedEndpoint[httpkernel.UserPath, httpquery.SearchInput, foundryhttp.NoBody, httpdto.UserResponse]
	Asset   foundryhttp.SignedRoute[httpkernel.AssetPath]
}

func NewLinks(signer foundryhttp.URLSigner) (Links, error) {
	links := Links{Preview: Preview.Signed(signer), Asset: Asset.Signed(signer)}
	if err := links.Preview.Validate(); err != nil {
		return Links{}, err
	}
	if err := links.Asset.Validate(); err != nil {
		return Links{}, err
	}
	return links, nil
}
func (links Links) PreviewURL(ctx context.Context, origin foundryhttp.Origin, path httpkernel.UserPath, query httpquery.SearchInput, expires time.Time) (string, error) {
	return links.Preview.URL(ctx, origin, path, query, expires)
}
func (links Links) AssetURL(ctx context.Context, origin foundryhttp.Origin, path httpkernel.AssetPath, expires time.Time) (string, error) {
	return links.Asset.URL(ctx, origin, path, expires)
}

type Service interface {
	Preview(context.Context, PreviewRequest) (httpdto.UserResponse, error)
}

func (links Links) Handler(service Service, public foundryhttp.PublicURLConfig) (http.Handler, error) {
	router, err := foundryhttp.NewRouter(
		links.Preview.Handle(service.Preview),
		links.Asset.HandleRaw(func(w http.ResponseWriter, r *http.Request, path httpkernel.AssetPath) {
			// This fixture asserts path delivery, not a filesystem/storage adapter.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(path.File))
		}),
	)
	if err != nil {
		return nil, err
	}
	return foundryhttp.ApplyMiddleware(router, foundryhttp.PublicURLs(public))
}
