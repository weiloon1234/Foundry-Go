package http

import (
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// CSPPolicy describes one policy. Nil source slices omit a directive; an
// explicitly empty source slice emits 'none'. Sandbox has the same presence
// rule, with an empty slice enabling the full sandbox. A wholly empty policy
// is rejected. Browser enforcement and reporting remain browser responsibilities.
type CSPPolicy struct {
	DefaultSrc, ScriptSrc, ScriptSrcElem, ScriptSrcAttr []CSPSource
	StyleSrc, StyleSrcElem, StyleSrcAttr                []CSPSource
	ChildSrc, ConnectSrc, FontSrc, FrameSrc, ImgSrc     []CSPSource
	ManifestSrc, MediaSrc, ObjectSrc, WorkerSrc         []CSPSource
	BaseURI, FormAction, FrameAncestors                 []CSPSource
	Sandbox                                             []CSPSandboxToken
	UpgradeInsecureRequests                             bool
	ReportTo                                            CSPReportGroup
	ReportURI                                           []CSPReportURI
}

// CSPSandboxToken is one explicit permission granted back to a CSP sandbox.
type CSPSandboxToken string

const (
	CSPSandboxDownloads                    CSPSandboxToken = "allow-downloads"
	CSPSandboxForms                        CSPSandboxToken = "allow-forms"
	CSPSandboxModals                       CSPSandboxToken = "allow-modals"
	CSPSandboxOrientationLock              CSPSandboxToken = "allow-orientation-lock"
	CSPSandboxPointerLock                  CSPSandboxToken = "allow-pointer-lock"
	CSPSandboxPopups                       CSPSandboxToken = "allow-popups"
	CSPSandboxPopupsEscape                 CSPSandboxToken = "allow-popups-to-escape-sandbox"
	CSPSandboxPresentation                 CSPSandboxToken = "allow-presentation"
	CSPSandboxSameOrigin                   CSPSandboxToken = "allow-same-origin"
	CSPSandboxScripts                      CSPSandboxToken = "allow-scripts"
	CSPSandboxStorageAccess                CSPSandboxToken = "allow-storage-access-by-user-activation"
	CSPSandboxTopNavigation                CSPSandboxToken = "allow-top-navigation"
	CSPSandboxTopNavigationActivation      CSPSandboxToken = "allow-top-navigation-by-user-activation"
	CSPSandboxTopNavigationCustomProtocols CSPSandboxToken = "allow-top-navigation-to-custom-protocols"
)

func (t CSPSandboxToken) Validate() error {
	switch t {
	case CSPSandboxDownloads, CSPSandboxForms, CSPSandboxModals, CSPSandboxOrientationLock, CSPSandboxPointerLock,
		CSPSandboxPopups, CSPSandboxPopupsEscape, CSPSandboxPresentation, CSPSandboxSameOrigin, CSPSandboxScripts,
		CSPSandboxStorageAccess, CSPSandboxTopNavigation, CSPSandboxTopNavigationActivation, CSPSandboxTopNavigationCustomProtocols:
		return nil
	}
	return fault.New(fault.Invalid, "invalid content security policy sandbox token")
}

// CSPReportGroup names a Reporting-Endpoints group configured separately.
type CSPReportGroup string

// CSPReportURI is a legacy report-uri destination: an absolute HTTP(S) URL or
// absolute local path. Reporting endpoint handlers must be registered separately.
type CSPReportURI string

func (p CSPPolicy) Validate() error { _, err := compileCSPPolicy(p, false); return err }

type compiledCSP struct {
	text  string
	nonce bool
}

func compileCSPPolicy(p CSPPolicy, reportOnly bool) (compiledCSP, error) {
	var result compiledCSP
	parts := make([]string, 0, 24)
	sourceCount := 0
	for _, directive := range []struct {
		name    string
		sources []CSPSource
	}{
		{"default-src", p.DefaultSrc}, {"script-src", p.ScriptSrc}, {"script-src-elem", p.ScriptSrcElem}, {"script-src-attr", p.ScriptSrcAttr},
		{"style-src", p.StyleSrc}, {"style-src-elem", p.StyleSrcElem}, {"style-src-attr", p.StyleSrcAttr},
		{"child-src", p.ChildSrc}, {"connect-src", p.ConnectSrc}, {"font-src", p.FontSrc}, {"frame-src", p.FrameSrc}, {"img-src", p.ImgSrc},
		{"manifest-src", p.ManifestSrc}, {"media-src", p.MediaSrc}, {"object-src", p.ObjectSrc}, {"worker-src", p.WorkerSrc},
		{"base-uri", p.BaseURI}, {"form-action", p.FormAction}, {"frame-ancestors", p.FrameAncestors},
	} {
		if directive.sources == nil {
			continue
		}
		sourceCount += len(directive.sources)
		if sourceCount > 256 {
			return compiledCSP{}, fault.New(fault.Invalid, "content security policy exceeds its source bound")
		}
		values := make([]string, 0, len(directive.sources))
		seen := make(map[string]bool, len(directive.sources))
		for _, source := range directive.sources {
			if err := source.Validate(); err != nil {
				return compiledCSP{}, err
			}
			if source.text == "'none'" && len(directive.sources) != 1 {
				return compiledCSP{}, fault.New(fault.Invalid, "CSP none must be the only source")
			}
			if directive.name == "frame-ancestors" && source.kind != cspHost && source.kind != cspScheme && source.text != "'self'" && source.text != "'none'" {
				return compiledCSP{}, fault.New(fault.Invalid, "frame ancestors require host, scheme, self or none sources")
			}
			if source.kind == cspNonce {
				switch directive.name {
				case "default-src", "script-src", "script-src-elem", "style-src", "style-src-elem":
				default:
					return compiledCSP{}, fault.New(fault.Invalid, "CSP nonce requires a script or style element source directive")
				}
				result.nonce = true
			}
			if seen[source.text] {
				return compiledCSP{}, fault.New(fault.Duplicate, "content security policy source is repeated")
			}
			seen[source.text] = true
			values = append(values, source.text)
		}
		if len(values) == 0 {
			values = append(values, "'none'")
		}
		parts = append(parts, directive.name+" "+strings.Join(values, " "))
	}
	if p.Sandbox != nil {
		if reportOnly {
			return compiledCSP{}, fault.New(fault.Invalid, "CSP sandbox cannot run in report-only mode")
		}
		if len(p.Sandbox) > 32 {
			return compiledCSP{}, fault.New(fault.Invalid, "CSP sandbox exceeds its token bound")
		}
		text := "sandbox"
		seen := make(map[CSPSandboxToken]bool)
		for _, token := range p.Sandbox {
			if err := token.Validate(); err != nil {
				return compiledCSP{}, err
			}
			if seen[token] {
				return compiledCSP{}, fault.New(fault.Duplicate, "CSP sandbox token is repeated")
			}
			seen[token] = true
			text += " " + string(token)
		}
		parts = append(parts, text)
	}
	if p.UpgradeInsecureRequests {
		if reportOnly {
			return compiledCSP{}, fault.New(fault.Invalid, "upgrade-insecure-requests requires an enforced policy")
		}
		parts = append(parts, "upgrade-insecure-requests")
	}
	if p.ReportTo != "" {
		if len(p.ReportTo) > 256 {
			return compiledCSP{}, fault.New(fault.Invalid, "CSP report group exceeds its name bound")
		}
		for i := range len(p.ReportTo) {
			b := p.ReportTo[i]
			if !asciiLetter(b) && !(b >= '0' && b <= '9') && b != '-' && b != '_' {
				return compiledCSP{}, fault.New(fault.Invalid, "invalid CSP report group")
			}
		}
		parts = append(parts, "report-to "+string(p.ReportTo))
	}
	if len(p.ReportURI) > 16 {
		return compiledCSP{}, fault.New(fault.Invalid, "CSP report destinations exceed their bound")
	}
	if len(p.ReportURI) > 0 {
		values := make([]string, 0, len(p.ReportURI))
		seen := make(map[CSPReportURI]bool)
		for _, endpoint := range p.ReportURI {
			if !validCSPReportURI(string(endpoint)) {
				return compiledCSP{}, fault.New(fault.Invalid, "invalid CSP report destination")
			}
			if seen[endpoint] {
				return compiledCSP{}, fault.New(fault.Duplicate, "CSP report destination is repeated")
			}
			seen[endpoint] = true
			values = append(values, string(endpoint))
		}
		parts = append(parts, "report-uri "+strings.Join(values, " "))
	}
	result.text = strings.Join(parts, "; ")
	if result.text == "" || len(result.text)+strings.Count(result.text, "\x00")*(cspNonceSourceBytes-1) > MaxHeaderValueBytes {
		return compiledCSP{}, fault.New(fault.Invalid, "content security policy is empty or exceeds its header bound")
	}
	return result, nil
}

func validCSPReportURI(text string) bool {
	if len(text) == 0 || len(text) > 2048 {
		return false
	}
	for i := range len(text) {
		if text[i] <= ' ' || text[i] >= 127 || strings.ContainsRune(";,\\", rune(text[i])) {
			return false
		}
	}
	u, err := url.Parse(text)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if strings.HasPrefix(text, "/") {
		return !strings.HasPrefix(text, "//") && u.Host == "" && u.Scheme == ""
	}
	return (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != ""
}
