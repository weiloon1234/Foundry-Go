// Package httpetags verifies automatic validators around a typed consumer endpoint.
package httpetags

import (
	"context"
	stdhttp "net/http"

	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/mutatorqueries"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type MemberPath struct {
	Member model.ID[mutatorqueries.Member]
}
type Request = foundryhttp.Input[MemberPath, foundryhttp.NoQuery, foundryhttp.NoBody]

var Show = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(
		foundryhttp.RouteSpec{ID: "etag.members.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
		foundryhttp.DefinePath("/members/{member}", foundryhttp.Param("member",
			foundryhttp.ModelIDPath[mutatorqueries.Member](),
			func(p *MemberPath) *model.ID[mutatorqueries.Member] { return &p.Member })),
	),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
	foundryhttp.JSONResponse(200, httppagination.MemberResponseJSON()),
)

type Directory interface {
	Find(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error)
}

// Handler reuses the existing typed DTO presenter, including its explicit getter
// choices. Foundry owns body hashing, request conditions and writer capabilities.
func Handler(directory Directory) (stdhttp.Handler, error) {
	router, err := foundryhttp.NewRouter(Show.Handle(func(ctx context.Context, in Request) (httppagination.MemberResponse, error) {
		member, err := directory.Find(ctx, in.Path.Member)
		if err != nil {
			return httppagination.MemberResponse{}, err
		}
		return httppagination.PresentMember(member)
	}))
	if err != nil {
		return nil, err
	}
	config := foundryhttp.DefaultETagConfig()
	return foundryhttp.ApplyMiddleware(router, foundryhttp.ETags(config))
}
