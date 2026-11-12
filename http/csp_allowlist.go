package http

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// CSPPreset selects a framework-owned set of exact HTTPS origins.
// Only the declared constants are accepted by Validate.
type CSPPreset string

// CSPPresetCloudflareWebAnalytics permits the Cloudflare beacon script and RUM
// connection origins. It does not enable Cloudflare injection or inline scripts.
const CSPPresetCloudflareWebAnalytics CSPPreset = "cloudflare-web-analytics"

// Validate rejects unknown presets, including the empty value.
func (p CSPPreset) Validate() error { _, err := p.allowlist(); return err }

func (p CSPPreset) allowlist() (CSPAllowlist, error) {
	switch p {
	case CSPPresetCloudflareWebAnalytics:
		return CSPAllowlist{
			ScriptOrigins:  []Origin{"https://static.cloudflareinsights.com"},
			ConnectOrigins: []Origin{"https://cloudflareinsights.com"},
		}, nil
	default:
		return CSPAllowlist{}, safeerror.NewFault(fault.Invalid, "unknown content security policy preset")
	}
}

// CSPAllowlist adds exact HTTPS hosts to explicitly declared CSP directives.
// Zero allows no additional hosts. Origins cannot contain paths, wildcards,
// credentials, queries, fragments, schemes alone or CSP keywords. Origin and
// CSP host-source validation both apply, so IPv6 literals are not supported.
type CSPAllowlist struct {
	Presets        []CSPPreset
	ScriptOrigins  []Origin
	ConnectOrigins []Origin
	ImageOrigins   []Origin
	FrameOrigins   []Origin
}

// Validate checks presets, origins and the existing CSP source/header bounds.
// Application settings validation calls it before acquiring resources.
func (a CSPAllowlist) Validate() error { _, err := a.policy(); return err }

func (a CSPAllowlist) policy() (CSPPolicy, error) {
	p := CSPPolicy{ScriptSrc: []CSPSource{}, ConnectSrc: []CSPSource{}, ImgSrc: []CSPSource{}, FrameSrc: []CSPSource{}}
	for _, preset := range a.Presets {
		allowlist, err := preset.allowlist()
		if err != nil {
			return CSPPolicy{}, err
		}
		if err := allowlist.appendOrigins(&p); err != nil {
			return CSPPolicy{}, err
		}
	}
	if err := a.appendOrigins(&p); err != nil {
		return CSPPolicy{}, err
	}
	if err := p.Validate(); err != nil {
		return CSPPolicy{}, err
	}
	return p, nil
}

func (a CSPAllowlist) appendOrigins(p *CSPPolicy) error {
	for _, directive := range []struct {
		name    string
		origins []Origin
		sources *[]CSPSource
	}{
		{"script-src", a.ScriptOrigins, &p.ScriptSrc},
		{"connect-src", a.ConnectOrigins, &p.ConnectSrc},
		{"img-src", a.ImageOrigins, &p.ImgSrc},
		{"frame-src", a.FrameOrigins, &p.FrameSrc},
	} {
		for _, origin := range directive.origins {
			parsed, err := ParseOrigin(string(origin))
			if err != nil || !strings.HasPrefix(string(parsed), "https://") {
				return safeerror.NewFault(fault.Invalid, "CSP allowlist "+directive.name+" requires an exact HTTPS origin")
			}
			source := CSPHostSource(string(parsed))
			if err := source.Validate(); err != nil {
				return safeerror.WrapFault(fault.Invalid, "invalid CSP allowlist "+directive.name+" host", err)
			}
			if !slices.Contains(*directive.sources, source) {
				*directive.sources = append(*directive.sources, source)
			}
		}
	}
	return nil
}

// ApplyCSPAllowlist returns an owned copy of policy with preset origins followed
// by custom origins appended to ScriptSrc, ConnectSrc, ImgSrc and FrameSrc.
// The input policy must be valid. A needed nil target directive is an error.
// An explicit empty or sole CSPNone directive becomes only the allowed hosts.
// Duplicate additions, including canonical origin equivalents already in the
// policy, are ignored.
// When script additions apply and both WorkerSrc and ChildSrc are nil, WorkerSrc
// is pinned to the original ScriptSrc so workers do not inherit added hosts.
// Explicit WorkerSrc/ChildSrc and other directives (including ScriptSrcElem)
// are unchanged. All slices are copied.
// Invalid input or a result exceeding CSP bounds returns a zero policy and error.
func ApplyCSPAllowlist(policy CSPPolicy, allowlist CSPAllowlist) (CSPPolicy, error) {
	if err := policy.Validate(); err != nil {
		return CSPPolicy{}, err
	}
	addition, err := allowlist.policy()
	if err != nil {
		return CSPPolicy{}, err
	}
	policy = cloneCSPPolicy(policy)
	if len(addition.ScriptSrc) > 0 && policy.WorkerSrc == nil && policy.ChildSrc == nil {
		policy.WorkerSrc = slices.Clone(policy.ScriptSrc)
	}
	for _, directive := range []struct {
		name    string
		sources *[]CSPSource
		added   []CSPSource
	}{
		{"script-src", &policy.ScriptSrc, addition.ScriptSrc},
		{"connect-src", &policy.ConnectSrc, addition.ConnectSrc},
		{"img-src", &policy.ImgSrc, addition.ImgSrc},
		{"frame-src", &policy.FrameSrc, addition.FrameSrc},
	} {
		if len(directive.added) == 0 {
			continue
		}
		if *directive.sources == nil {
			return CSPPolicy{}, safeerror.NewFault(fault.Invalid, "CSP allowlist requires an explicit "+directive.name+" directive")
		}
		if len(*directive.sources) == 1 && (*directive.sources)[0] == CSPNone() {
			*directive.sources = (*directive.sources)[:0]
		}
		seen := make(map[CSPSource]bool, len(*directive.sources))
		for _, source := range *directive.sources {
			if source.kind == cspHost {
				if origin, err := ParseOrigin(source.text); err == nil {
					source = CSPHostSource(string(origin))
				}
			}
			seen[source] = true
		}
		for _, source := range directive.added {
			if !seen[source] {
				*directive.sources = append(*directive.sources, source)
				seen[source] = true
			}
		}
	}
	if err := policy.Validate(); err != nil {
		return CSPPolicy{}, err
	}
	return policy, nil
}

func cloneCSPPolicy(p CSPPolicy) CSPPolicy {
	for _, sources := range []*[]CSPSource{
		&p.DefaultSrc, &p.ScriptSrc, &p.ScriptSrcElem, &p.ScriptSrcAttr,
		&p.StyleSrc, &p.StyleSrcElem, &p.StyleSrcAttr, &p.ChildSrc,
		&p.ConnectSrc, &p.FontSrc, &p.FrameSrc, &p.ImgSrc, &p.ManifestSrc,
		&p.MediaSrc, &p.ObjectSrc, &p.WorkerSrc, &p.BaseURI, &p.FormAction, &p.FrameAncestors,
	} {
		*sources = slices.Clone(*sources)
	}
	p.Sandbox = slices.Clone(p.Sandbox)
	p.ReportURI = slices.Clone(p.ReportURI)
	return p
}
