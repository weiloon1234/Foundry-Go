package teamworkflow

import (
	"context"

	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

// ReadCaptures models an application's developer type gate: only alice may
// read captured webhook payloads.
var ReadCaptures = auth.DefinePermission("workflow.captures.read", func(_ context.Context, actor Actor) (bool, error) {
	return actor.ID == 1, nil
})

// Webhooks captures gateway callbacks except the credential-bearing auth subtree.
// doc-example:begin webhook-capture-declaration
func Webhooks() ([]foundryhttp.CaptureDeclaration, error) {
	prefix, err := foundryhttp.ParsePathPrefix("/wh")
	if err != nil {
		return nil, err
	}
	auth, err := foundryhttp.ParsePathPrefix("/wh/stream-auth")
	if err != nil {
		return nil, err
	}
	return []foundryhttp.CaptureDeclaration{
		foundryhttp.CapturePrefix(prefix),
		foundryhttp.CapturePrefix(auth, foundryhttp.ExcludeFromCapture()),
	}, nil
}

// doc-example:end webhook-capture-declaration

// CaptureRoutes mounts the framework viewer behind the developer gate.
// doc-example:begin webhook-capture-routes
func CaptureRoutes(captures *capture.Store) ([]foundryhttp.RouteRegistration, error) {
	transport, guard, err := authfixture.Authentication(ReadCaptures.Registration())
	if err != nil {
		return nil, err
	}
	binding, err := foundryhttp.BindGuard(transport, guard)
	if err != nil {
		return nil, err
	}
	return capture.Routes(foundryhttp.DefineScope("/developer", "developer"), captures, binding, foundryhttp.Permissions(ReadCaptures))
}

// doc-example:end webhook-capture-routes
