package endpoint_authenticated

import (
	"context"
	stdhttp "net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type Actor struct {
	ID   string
	Name string
}

type ItemPath struct {
	ID string
}

func itemPathDescriptor() foundryhttp.Path[ItemPath] {
	return foundryhttp.DefinePath("/items/{id}",
		foundryhttp.Param("id", foundryhttp.StringPath[string](), func(p *ItemPath) *string { return &p.ID }),
	)
}

type ItemQuery struct {
	Search string `query:"search"`
}

func itemQueryDescriptor() foundryhttp.Query[ItemQuery] {
	return foundryhttp.DefineQuery(
		foundryhttp.QueryParam("search", foundryhttp.StringQuery[string](), func(q *ItemQuery) *string { return &q.Search }),
	)
}

type ItemBody struct {
	Name string `json:"name"`
}

func itemBodyJSON() contract.JSON[ItemBody] {
	return contract.DefineJSON[ItemBody](contract.Schema{
		Root: "foundry.test/consumer/testdata/footprint/endpoint_authenticated.ItemBody",
		Types: []contract.Type{
			{
				ID:   "foundry.test/consumer/testdata/footprint/endpoint_authenticated.ItemBody",
				Kind: contract.ObjectKind,
				Properties: []contract.Property{
					{Name: "name", Type: "string", Required: true},
				},
			},
			{ID: "string", Kind: contract.StringKind},
		},
	})
}

func itemBodyRules() validation.Rule[ItemBody] {
	nameField := validation.DefineField("name", func(v ItemBody) string { return v.Name })
	return validation.All(validation.Rules(nameField, validation.MinLength[string](1)))
}

type ItemReply struct {
	Message string `json:"message"`
}

func itemReplyJSON() contract.JSON[ItemReply] {
	return contract.DefineJSON[ItemReply](contract.Schema{
		Root: "foundry.test/consumer/testdata/footprint/endpoint_authenticated.ItemReply",
		Types: []contract.Type{
			{
				ID:   "foundry.test/consumer/testdata/footprint/endpoint_authenticated.ItemReply",
				Kind: contract.ObjectKind,
				Properties: []contract.Property{
					{Name: "message", Type: "string", Required: true},
				},
			},
			{ID: "string", Kind: contract.StringKind},
		},
	})
}

var (
	auditMiddleware = foundryhttp.DefineMiddleware("probe.audit", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return next, nil
	})
	deniedError = foundryhttp.DefineError("probe.denied", 409, "Probe conflict")
)

func Route(binding foundryhttp.GuardBinding[Actor], scopes auth.AccessScopes[Actor], permission auth.Permission[Actor]) foundryhttp.RouteRegistration {
	scope := foundryhttp.DefineScope("/v1", "probe")
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(
			foundryhttp.RouteSpec{
				ID:     "items.guarded",
				Method: foundryhttp.POST,
				Access: foundryhttp.Guarded,
			},
			itemPathDescriptor(),
		),
		itemQueryDescriptor(),
		foundryhttp.JSONBody(itemBodyJSON()),
		foundryhttp.JSONResponse(200, itemReplyJSON()),
		foundryhttp.Within(scope),
		foundryhttp.Timeout(5*time.Second),
		foundryhttp.Errors(deniedError),
		foundryhttp.Use(auditMiddleware),
	)
	validated := foundryhttp.ValidateBody(endpoint, itemBodyRules())
	secured := foundryhttp.Authenticated(validated, binding, foundryhttp.Scopes(scopes), foundryhttp.Permissions(permission))
	return secured.Handle(func(_ context.Context, _ Actor, in foundryhttp.Input[ItemPath, ItemQuery, ItemBody]) (ItemReply, error) {
		return ItemReply{Message: in.Body.Name}, nil
	})
}
