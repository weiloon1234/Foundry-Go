package httpsecurity

import (
	"net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AllowlistedPageHandler keeps the application policy explicit and takes the
// framework-owned allowlist from the loaded HTTP settings.
func AllowlistedPageHandler(settings application.HTTPSettings) (http.Handler, error) {
	policy, err := foundryhttp.ApplyCSPAllowlist(foundryhttp.CSPPolicy{
		DefaultSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ScriptSrc:  []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ConnectSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ImgSrc:     []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		FrameSrc:   []foundryhttp.CSPSource{foundryhttp.CSPNone()},
		ObjectSrc:  []foundryhttp.CSPSource{foundryhttp.CSPNone()},
	}, settings.CSPAllowlist)
	if err != nil {
		return nil, err
	}
	return foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Allowlisted page</title>"))
	}), foundryhttp.ContentSecurityPolicy(foundryhttp.CSPConfig{Enforce: value.Set(policy)}))
}
