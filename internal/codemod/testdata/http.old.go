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
	e := h.DefineEndpoint(r, 1, 2, 3).Within(s).WithTimeout(5).WithMiddleware(middleware...).WithBodyValidation(rules...)
	_ = e.WithTimeout(7)
	_ = e.Validate()
	_ = e.ID()
	_ = r.ID()
	_ = r.Within(s)
	_ = s.Within(s)
	_ = (Other{}).Validate()
	_ = e.Handle(0)
	_ = h.Authenticated(e, "actor").WithActorMiddleware(middleware...).WithPermissions("read")
	_ = b.Bind(e, 0)
	_ = b.BindAuthenticated(h.Authenticated(e, "actor"), 0)
	_ = Endpoint.Validate
}
