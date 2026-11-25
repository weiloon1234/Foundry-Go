package httpsecurity

import (
	"html/template"
	"net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func noncePolicy() foundryhttp.Middleware {
	// doc-example:begin http-content-security-policy-1
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
	// doc-example:end http-content-security-policy-1
	return middleware
}

func wasmPolicy() foundryhttp.CSPPolicy {
	// doc-example:begin http-content-security-policy-2
	policy := foundryhttp.CSPPolicy{
		DefaultSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
		ScriptSrc:  []foundryhttp.CSPSource{foundryhttp.CSPSelf(), foundryhttp.CSPWasmUnsafeEval()},
		ConnectSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	}
	// doc-example:end http-content-security-policy-2
	return policy
}

func analyticsSettings(settings application.Settings) application.Settings {
	// doc-example:begin http-content-security-policy-3
	settings.HTTP.CSPAllowlist.Presets = []foundryhttp.CSPPreset{
		foundryhttp.CSPPresetCloudflareWebAnalytics,
	}
	// doc-example:end http-content-security-policy-3
	return settings
}

func allowlistedHandler(settings application.Settings, router http.Handler) (http.Handler, error) {
	// doc-example:begin http-content-security-policy-4
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
	// doc-example:end http-content-security-policy-4
}

func renderPage(writer http.ResponseWriter, request *http.Request, page *template.Template) {
	// doc-example:begin http-content-security-policy-5
	nonce, ok := foundryhttp.CSPNonceFromContext(request.Context())
	if !ok {
		// This handler requires a configured nonce policy.
		return
	}
	data := struct{ Nonce string }{Nonce: nonce.String()}
	if err := page.Execute(writer, data); err != nil {
		return
	}
	// doc-example:end http-content-security-policy-5
}
