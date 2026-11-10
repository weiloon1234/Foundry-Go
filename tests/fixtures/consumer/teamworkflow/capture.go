package teamworkflow

import (
	"context"

	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

// ReadCaptures models an application's developer type gate: only alice may
// read captured webhook payloads.
var ReadCaptures = auth.DefinePermission("workflow.captures.read", func(_ context.Context, actor Actor) (bool, error) {
	return actor.ID == 1, nil
})

// Webhooks declares the gateway callback prefix. Unmatched paths under it are
// captured too, so no placeholder handler is needed.
// doc-example:begin webhook-capture-declaration
func Webhooks() (http.CaptureDeclaration, error) {
	prefix, err := http.ParsePathPrefix("/wh")
	return http.CapturePrefix(prefix), err
} // doc-example:end webhook-capture-declaration

// CaptureRoutes mounts the framework viewer behind the developer gate.
// doc-example:begin webhook-capture-routes
func CaptureRoutes(captures *capture.Store) ([]http.RouteRegistration, error) {
	transport, guard, err := authfixture.Authentication(ReadCaptures.Registration())
	if err != nil {
		return nil, err
	}
	binding, err := http.BindGuard(transport, guard)
	if err != nil {
		return nil, err
	}
	return capture.Routes(http.DefineScope("/developer", "developer"), captures, binding, http.Permissions(ReadCaptures))
} // doc-example:end webhook-capture-routes
