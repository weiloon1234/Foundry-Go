package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// Snapshot copies caller-owned slices before a server/module retains config.
func (c ServerConfig) Snapshot() ServerConfig {
	c.MaintenanceReadPaths = slices.Clone(c.MaintenanceReadPaths)
	return c
}

func (c ServerConfig) validateMaintenancePaths() error {
	if len(c.MaintenanceReadPaths) > 16 {
		return fault.New(fault.Invalid, "too many maintenance read paths")
	}
	seen := make(map[string]bool, len(c.MaintenanceReadPaths))
	for _, path := range c.MaintenanceReadPaths {
		if path == "" || len(path) > 1024 {
			return fault.New(fault.Invalid, "invalid maintenance read path")
		}
		canonical, err := StaticPath(path).URL(NoPath{})
		if err != nil {
			return err
		}
		if canonical != path {
			return fault.New(fault.Invalid, "maintenance paths must be canonical unescaped static paths")
		}
		if seen[path] {
			return fault.New(fault.Duplicate, "duplicate maintenance read path")
		}
		seen[path] = true
	}
	return nil
}

func (c ServerConfig) permitsMaintenanceRead(request *stdhttp.Request) bool {
	if request.Method != stdhttp.MethodGet && request.Method != stdhttp.MethodHead || request.URL == nil || request.URL.RawPath != "" {
		return false
	}
	return slices.Contains(c.MaintenanceReadPaths, request.URL.Path)
}

// WithAdmissionProxy lets kernel-level admission, which runs before the
// middleware chain, resolve the client address and scheme exactly as the given
// TrustedProxy middleware will: maintenance allow networks then match the
// client behind a trusted load balancer instead of the balancer itself, and the
// bypass cookie is Secure when the trusted public scheme is https. Without it
// admission uses the socket peer and native TLS. Configured applications pass
// their global TrustedProxy automatically.
func WithAdmissionProxy(proxy Middleware) ServerOption {
	return func(o *serverOptions) error {
		if proxy.id != TrustedProxyMiddlewareID || proxy.proxy == nil {
			return fault.New(fault.Invalid, "admission proxy requires a valid TrustedProxy middleware")
		}
		o.proxy = proxy.proxy
		return nil
	}
}

// maintenanceDecision is computed once at admission. A paused gate admits
// configured read paths, configured/shared exemptions, allowed client networks
// and holders of a valid signed bypass cookie. Draining admits nothing.
type maintenanceDecision struct {
	admitted bool
	paused   bool
	state    maintenance.State
	bypass   string
	expires  time.Time
	secure   bool
}

func admitMaintenance(r *stdhttp.Request, config ServerConfig, proxy *proxyPolicy) maintenanceDecision {
	gate := maintenance.FromContext(r.Context())
	if gate == nil {
		// Standalone servers may attach only an observation recorder.
		gate = observability.FromContext(r.Context()).Gate()
	}
	err := gate.Admit()
	if err == nil {
		return maintenanceDecision{admitted: true}
	}
	if errors.Is(err, maintenance.ErrDraining) || r.URL == nil {
		return maintenanceDecision{}
	}
	now := time.Now()
	client, secure := PeerIP(r), r.TLS != nil
	if proxy != nil {
		// The same parsing owner as TrustedProxy; invalid forwarded origin data
		// keeps the native scheme here and is rejected later by the middleware.
		client = proxy.clientIP(r)
		if origin, err := proxy.forwardedOrigin(r); err == nil && origin.scheme != "" {
			secure = origin.scheme == "https"
		}
	}
	if config.permitsMaintenanceRead(r) || r.URL.RawPath == "" && gate.Exempts(r.Method, r.URL.Path, client) {
		return maintenanceDecision{admitted: true}
	}
	if cookie, err := r.Cookie(maintenance.BypassCookie); err == nil && gate.Bypass(r.Context(), cookie.Value, now) {
		return maintenanceDecision{admitted: true}
	}
	// Visiting /<secret> exchanges the shared secret for a signed cookie.
	if (r.Method == stdhttp.MethodGet || r.Method == stdhttp.MethodHead) && r.URL.RawPath == "" && r.URL.RawQuery == "" && len(r.URL.Path) > 1 && strings.LastIndexByte(r.URL.Path, '/') == 0 {
		if value, expires, ok := gate.IssueBypass(r.Context(), r.URL.Path[1:], now); ok {
			return maintenanceDecision{bypass: value, expires: expires, secure: secure}
		}
	}
	return maintenanceDecision{paused: true, state: gate.State()}
}

// reject writes the maintenance response, the bypass exchange, or the ordinary
// unavailable response for draining and capacity rejection. Maintenance is an
// operator decision rather than a server failure, so it is not logged per request.
func (d maintenanceDecision) reject(w stdhttp.ResponseWriter, r *stdhttp.Request, logger *slog.Logger) {
	header := w.Header()
	if d.bypass != "" {
		stdhttp.SetCookie(w, &stdhttp.Cookie{Name: maintenance.BypassCookie, Value: d.bypass, Path: "/", Expires: d.expires, MaxAge: int(time.Until(d.expires) / time.Second), HttpOnly: true, Secure: d.secure, SameSite: stdhttp.SameSiteLaxMode})
		header.Set("Cache-Control", "no-store")
		header.Set("Location", "/")
		w.WriteHeader(stdhttp.StatusSeeOther)
		return
	}
	if !d.paused {
		writeRequestError(w, r, Unavailable, logger)
		return
	}
	definition, _ := Unavailable.definition()
	payload := ErrorResponse{Status: definition.Status, Code: definition.Code, Message: definition.Message, RequestID: RequestID(r.Context())}
	if d.state.Message != "" {
		payload.Message = d.state.Message
	}
	body, err := json.Marshal(payload)
	if err != nil {
		writeRequestError(w, r, Unavailable, logger)
		return
	}
	clearResponseRepresentation(header)
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	if d.state.RetryAfter > 0 {
		header.Set("Retry-After", strconv.FormatInt(int64(d.state.RetryAfter/time.Second), 10))
	}
	w.WriteHeader(payload.Status)
	if r.Method != stdhttp.MethodHead {
		_, _ = w.Write(append(body, '\n'))
	}
}
