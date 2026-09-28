package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Registry returns the immutable declaration/capacity owner for another typed
// transport adapter. It does not authenticate a request or expose credentials.
func (a *Authentication) Registry() *auth.Registry {
	if a == nil {
		return nil
	}
	return a.registry
}

// CaptureCredentials snapshots the declared HTTP inputs without retaining the
// request or a resolved subject. A long-lived transport must create and close a
// fresh Registry().NewScope for every authorization boundary. Browser session
// capture reuses secure-cookie validation; it neither creates nor rotates a
// session. Upgraded transports must independently enforce their handshake origin
// policy: safe-method CSRF rules are not WebSocket authorization.
func (a *Authentication) CaptureCredentials(r *stdhttp.Request) (auth.Credentials, error) {
	if err := a.validate(); err != nil {
		return auth.Credentials{}, err
	}
	if r == nil || r.URL == nil {
		return auth.Credentials{}, fault.New(fault.Invalid, "credential capture requires a request")
	}
	if err := r.Context().Err(); err != nil {
		return auth.Credentials{}, err
	}
	if a.browser == nil {
		return a.inputs(r)
	}
	credential, err := a.browser.readCredential(r)
	if err != nil {
		return auth.Credentials{}, err
	}
	if token, present := credential.Get(); present {
		return auth.NewCredentials(auth.Credential{Name: a.sources[0].name, Secret: token})
	}
	return auth.NewCredentials()
}
