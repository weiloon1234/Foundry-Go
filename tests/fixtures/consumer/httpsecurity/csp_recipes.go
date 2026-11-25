package httpsecurity

import (
	"html/template"
	"net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin http-content-security-policy-1
func noncePolicy() foundryhttp.Middleware {
	policy := foundryhttp.CSPPolicy{
		DefaultSrc:     []foundryhttp.CSPSource{foundryhttp.CSPNone()},
		ScriptSrc:      []foundryhttp.CSPSource{foundryhttp.CSPNonceSource(), foundryhttp.CSPStrictDynamic()},
		ImgSrc:         []foundryhttp.CSPSource{foundryhttp.CSPSelf(), foundryhttp.CSPSchemeSource("data")},
		ConnectSrc:     []foundryhttp.CSPSource{foundryhttp.CSPHostSource("wss://events.example.test")},
		BaseURI:        []foundryhttp.CSPSource{foundryhttp.CSPNone()},
		FrameAncestors: []foundryhttp.CSPSource{foundryhttp.CSPNone()},
	}
	middleware := foundryhttp.ContentSecurityPolicy(foundryhttp.CSPConfig{
		Enforce: value.Set(policy),
	})
	return middleware
}

// doc-example:end http-content-security-policy-1
// doc-example:begin http-content-security-policy-2
func wasmPolicy() foundryhttp.CSPPolicy {
	policy := foundryhttp.CSPPolicy{
		DefaultSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ScriptSrc:  []foundryhttp.CSPSource{foundryhttp.CSPSelf(), foundryhttp.CSPWasmUnsafeEval()},
		ConnectSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	}
	return policy
}

// doc-example:end http-content-security-policy-2
// doc-example:begin http-content-security-policy-3
func analyticsSettings(settings application.Settings) application.Settings {
	settings.HTTP.CSPAllowlist.Presets = []foundryhttp.CSPPreset{
		foundryhttp.CSPPresetCloudflareWebAnalytics,
	}
	return settings
}

// doc-example:end http-content-security-policy-3
// doc-example:begin http-content-security-policy-4
func allowlistedHandler(settings application.Settings, router http.Handler) (http.Handler, error) {
	policy, err := foundryhttp.ApplyCSPAllowlist(foundryhttp.CSPPolicy{
		DefaultSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ScriptSrc:  []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ConnectSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ImgSrc:     []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		FrameSrc:   []foundryhttp.CSPSource{foundryhttp.CSPNone()},
	}, settings.HTTP.CSPAllowlist)
	if err != nil {
		return nil, err
	}
	middleware := foundryhttp.ContentSecurityPolicy(foundryhttp.CSPConfig{
		Enforce: value.Set(policy),
	})
	return foundryhttp.ApplyMiddleware(router, middleware)
}

// doc-example:end http-content-security-policy-4
// doc-example:begin http-content-security-policy-5
func renderPage(writer http.ResponseWriter, request *http.Request, page *template.Template) {
	nonce, ok := foundryhttp.CSPNonceFromContext(request.Context())
	if !ok {
		// This handler requires a configured nonce policy.
		return
	}
	data := struct{ Nonce string }{Nonce: nonce.String()}
	_ = page.Execute(writer, data)

}

// doc-example:end http-content-security-policy-5
