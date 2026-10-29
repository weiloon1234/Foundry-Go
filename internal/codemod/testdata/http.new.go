package consumer

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Endpoint = h.Endpoint[int, int, int, int]
type Other struct{}

func (Other) Validate() error { return nil }

func declarations(r h.Route[int], s h.Scope, middleware []int, rules []int) {
	// Options retain declaration order; typed validation stays outside.
	e := h.ValidateBody(h.DefineEndpoint(r, 1, 2, 3, h.Within(s), h.Timeout(5), h.Use(middleware...)), rules...)
	_ = h.ConfigureEndpoint(e, h.Timeout(7))
	_ = h.ValidateEndpoint(e)
	_ = h.EndpointID(e)
	_ = h.RouteIDOf(r)
	_ = h.ConfigureRoute(r, h.Within(s))
	_ = s.Within(s)
	_ = (Other{}).Validate()
	_ = e.Handle(0)
	_ = h.Authenticated(e, "actor", h.ActorMiddleware[string](middleware...), h.Permissions("read"))
	_ = b.Bind(h.ForBinding(e), 0)
	_ = b.BindAuthenticated(h.ForAuthenticatedBinding(h.Authenticated(e, "actor")), 0)
	_ = h.ValidateEndpoint[int, int, int, int]
}
