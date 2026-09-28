package httpsecurity

import (
	"html/template"
	"net/http"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PageHandler demonstrates native HTML rendering with a typed request nonce.
// Domain templates stay in the consumer; Foundry owns policy and nonce handling.
func PageHandler() (http.Handler, error) {
	page, err := template.New("page").Parse(`<!doctype html><title>Foundry</title><script nonce="{{.Nonce}}">document.title = "Foundry-Go";</script>`)
	if err != nil {
		return nil, err
	}
	policy := foundryhttp.CSPPolicy{
		DefaultSrc:     []foundryhttp.CSPSource{foundryhttp.CSPNone()},
		ScriptSrc:      []foundryhttp.CSPSource{foundryhttp.CSPNonceSource(), foundryhttp.CSPStrictDynamic()},
		BaseURI:        []foundryhttp.CSPSource{foundryhttp.CSPNone()},
		FrameAncestors: []foundryhttp.CSPSource{foundryhttp.CSPNone()},
	}
	policy.ReportTo = foundryhttp.CSPReportGroup("csp")
	config := foundryhttp.CSPConfig{
		Enforce: value.Set(policy),
		ReportOnly: value.Set(foundryhttp.CSPPolicy{
			ScriptSrc: []foundryhttp.CSPSource{foundryhttp.CSPNonceSource()},
			ReportURI: []foundryhttp.CSPReportURI{"/reports/csp"},
		}),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, ok := foundryhttp.CSPNonceFromContext(r.Context())
		if !ok {
			http.Error(w, "Missing content policy", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Reporting routes are separate application registrations; this fixture
		// only demonstrates the policy and corresponding trusted script tag.
		_ = page.Execute(w, struct{ Nonce string }{Nonce: nonce.String()})
	})
	return foundryhttp.ApplyMiddleware(handler,
		foundryhttp.SecurityHeaders(foundryhttp.DefaultSecurityHeadersConfig()),
		foundryhttp.ContentSecurityPolicy(config),
	)
}
