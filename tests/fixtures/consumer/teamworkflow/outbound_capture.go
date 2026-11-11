package teamworkflow

import (
	"context"
	"net/url"

	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
)

// OutboundCaptureRoutes mounts the framework's outbound call viewer behind the
// same developer gate as the webhook viewer.
// doc-example:begin outbound-capture-routes
func OutboundCaptureRoutes(captures *outboundcapture.Store) ([]http.RouteRegistration, error) {
	transport, guard, err := authfixture.Authentication(ReadCaptures.Registration())
	if err != nil {
		return nil, err
	}
	binding, err := http.BindGuard(transport, guard)
	if err != nil {
		return nil, err
	}
	return outboundcapture.Routes(http.DefineScope("/developer", "developer"), captures, binding, http.Permissions(ReadCaptures))
} // doc-example:end outbound-capture-routes

// SendCode texts a one-time code. The code is secret by meaning rather than
// by field name, so the capture row keeps the call but not its bodies.
// doc-example:begin outbound-capture-decline
func SendCode(ctx context.Context, sms *httpclient.Client, to, code string) error {
	request := sms.Post("/messages").Form(url.Values{"To": {to}, "Body": {code}}).WithoutBodyCapture()
	response, err := sms.Do(ctx, request)
	if err != nil {
		return err
	}
	return response.EnsureSuccess()
} // doc-example:end outbound-capture-decline
