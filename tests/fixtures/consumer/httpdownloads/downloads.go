// Package httpdownloads verifies a thin typed document endpoint in a separate
// consumer module. The framework owns transfer mechanics and resource cleanup.
package httpdownloads

import (
	"context"
	"errors"
	"os"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Request = foundryhttp.Input[httpkernel.UserPath, foundryhttp.NoQuery, foundryhttp.NoBody]

var Show = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "documents.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, httpkernel.UserPathDescriptor()),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("application/pdf", "text/plain; charset=utf-8"),
)

// Document is domain-owned metadata. Path comes from a trusted domain lookup;
// display names cannot select the filesystem object opened by Foundry.
type Document struct {
	Path      string
	Name      string
	MediaType foundryhttp.MediaType
	EntityTag foundryhttp.EntityTag
}
type Documents interface {
	File(context.Context, model.ID[models.User]) (Document, error)
}

func Router(root *os.Root, documents Documents) (*foundryhttp.Router, error) {
	if root == nil || documents == nil {
		return nil, errors.New("document router requires a root and service")
	}
	return foundryhttp.NewRouter(Show.Handle(func(ctx context.Context, input Request) (foundryhttp.Download, error) {
		document, err := documents.File(ctx, input.Path.User)
		if err != nil {
			return foundryhttp.Download{}, err
		}
		download := foundryhttp.LocalDownload(root, document.Path)
		return download.WithName(document.Name).WithMediaType(document.MediaType).WithEntityTag(document.EntityTag), nil
	}))
}
