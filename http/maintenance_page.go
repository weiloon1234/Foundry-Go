package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	stdhttp "net/http"
)

const maintenancePageStyle = `:root { color-scheme: light dark; --background: #fafafa; --text: #262626; --muted: #666; }
@media (prefers-color-scheme: dark) { :root { --background: #171717; --text: #eee; --muted: #aaa; } }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 2rem; background: var(--background); color: var(--text); font-family: system-ui, sans-serif; line-height: 1.6; }
main { width: 100%; max-width: 32rem; }
.status { color: var(--muted); font-size: .875rem; }
h1 { margin: .5rem 0; font-size: clamp(1.75rem, 6vw, 2.5rem); line-height: 1.2; }
p { overflow-wrap: anywhere; }`

const maintenancePageSource = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<meta name="color-scheme" content="light dark">
<title>Down for maintenance</title>
<style>` + maintenancePageStyle + `</style>
</head>
<body>
<main>
<div class="status">503</div>
<h1>Down for maintenance</h1>
<p>{{if .}}{{.}}{{else}}This site is temporarily unavailable for scheduled maintenance. Please check back soon.{{end}}</p>
</main>
</body>
</html>
`

var maintenancePageTemplate = template.Must(template.New("maintenance").Parse(maintenancePageSource))

var maintenancePageCSP = func() string {
	hash := sha256.Sum256([]byte(maintenancePageStyle))
	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}()

func writeMaintenancePage(w stdhttp.ResponseWriter, r *stdhttp.Request, message string) error {
	var body bytes.Buffer
	if err := maintenancePageTemplate.Execute(&body, message); err != nil {
		return err
	}
	header := w.Header()
	clearResponseRepresentation(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", maintenancePageCSP)
	w.WriteHeader(stdhttp.StatusServiceUnavailable)
	if r.Method != stdhttp.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
	return nil
}
