package http

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// CrossOriginOpenerPolicy selects Cross-Origin-Opener-Policy. Zero omits it.
// same-origin isolates the browsing context group from cross-origin popups and
// openers; OAuth or payment popups may need same-origin-allow-popups.
type CrossOriginOpenerPolicy string

const (
	OpenerSameOrigin            CrossOriginOpenerPolicy = "same-origin"
	OpenerSameOriginAllowPopups CrossOriginOpenerPolicy = "same-origin-allow-popups"
	OpenerNoOpenerAllowPopups   CrossOriginOpenerPolicy = "noopener-allow-popups"
	OpenerUnsafeNone            CrossOriginOpenerPolicy = "unsafe-none"
)

func (p CrossOriginOpenerPolicy) Validate() error {
	switch p {
	case "", OpenerSameOrigin, OpenerSameOriginAllowPopups, OpenerNoOpenerAllowPopups, OpenerUnsafeNone:
		return nil
	}
	return fault.New(fault.Invalid, "unsupported cross-origin opener policy")
}

// CrossOriginEmbedderPolicy selects Cross-Origin-Embedder-Policy. Zero omits it.
// require-corp or credentialless, with an opener policy of same-origin, enables
// cross-origin isolation; every embedded resource must then opt in.
type CrossOriginEmbedderPolicy string

const (
	EmbedderRequireCORP    CrossOriginEmbedderPolicy = "require-corp"
	EmbedderCredentialless CrossOriginEmbedderPolicy = "credentialless"
	EmbedderUnsafeNone     CrossOriginEmbedderPolicy = "unsafe-none"
)

func (p CrossOriginEmbedderPolicy) Validate() error {
	switch p {
	case "", EmbedderRequireCORP, EmbedderCredentialless, EmbedderUnsafeNone:
		return nil
	}
	return fault.New(fault.Invalid, "unsupported cross-origin embedder policy")
}

// CrossOriginResourcePolicy selects Cross-Origin-Resource-Policy, which limits
// which sites may load this response as a no-cors subresource. Zero omits it.
type CrossOriginResourcePolicy string

const (
	ResourceSameOrigin  CrossOriginResourcePolicy = "same-origin"
	ResourceSameSite    CrossOriginResourcePolicy = "same-site"
	ResourceCrossOrigin CrossOriginResourcePolicy = "cross-origin"
)

func (p CrossOriginResourcePolicy) Validate() error {
	switch p {
	case "", ResourceSameOrigin, ResourceSameSite, ResourceCrossOrigin:
		return nil
	}
	return fault.New(fault.Invalid, "unsupported cross-origin resource policy")
}

// PermissionFeature names one Permissions-Policy feature. The constants cover
// common powerful features; other registered features use the same type and
// are validated as lowercase tokens.
type PermissionFeature string

const (
	PermissionCamera          PermissionFeature = "camera"
	PermissionMicrophone      PermissionFeature = "microphone"
	PermissionGeolocation     PermissionFeature = "geolocation"
	PermissionPayment         PermissionFeature = "payment"
	PermissionUSB             PermissionFeature = "usb"
	PermissionFullscreen      PermissionFeature = "fullscreen"
	PermissionDisplayCapture  PermissionFeature = "display-capture"
	PermissionClipboardWrite  PermissionFeature = "clipboard-write"
	PermissionBrowsingTopics  PermissionFeature = "browsing-topics"
	PermissionPublicKeyCreate PermissionFeature = "publickey-credentials-create"
	PermissionPublicKeyGet    PermissionFeature = "publickey-credentials-get"
)

// PermissionDirective grants one feature to an allowlist. The zero allowlist
// disables the feature everywhere (feature=()). Any grants every origin (*)
// and excludes Self and Origins. Origins are exact HTTP(S) origins.
type PermissionDirective struct {
	Feature PermissionFeature
	Self    bool
	Any     bool
	Origins []Origin
}

const maxPermissionDirectives = 64
const maxPermissionOrigins = 32

// compilePermissionsPolicy serializes directives as a Structured Fields
// dictionary, for example: camera=(), geolocation=(self "https://maps.example").
func compilePermissionsPolicy(directives []PermissionDirective) (HeaderValue, error) {
	if len(directives) > maxPermissionDirectives {
		return "", fault.New(fault.Invalid, "permissions policy exceeds its directive bound")
	}
	seen := make(map[PermissionFeature]bool, len(directives))
	members := make([]string, 0, len(directives))
	for _, directive := range directives {
		if !validPermissionFeature(directive.Feature) {
			return "", fault.New(fault.Invalid, "permissions policy feature must be a lowercase token")
		}
		if seen[directive.Feature] {
			return "", fault.New(fault.Duplicate, "permissions policy feature is repeated")
		}
		seen[directive.Feature] = true
		if len(directive.Origins) > maxPermissionOrigins || directive.Any && (directive.Self || len(directive.Origins) != 0) {
			return "", fault.New(fault.Invalid, "permissions policy allowlist is invalid or unbounded")
		}
		if directive.Any {
			members = append(members, string(directive.Feature)+"=*")
			continue
		}
		items := make([]string, 0, len(directive.Origins)+1)
		if directive.Self {
			items = append(items, "self")
		}
		origins := make(map[Origin]bool, len(directive.Origins))
		for _, input := range directive.Origins {
			origin, err := ParseOrigin(string(input))
			if err != nil || origin == NullOrigin || origins[origin] {
				return "", fault.New(fault.Invalid, "permissions policy origins must be distinct HTTP(S) origins")
			}
			origins[origin] = true
			items = append(items, `"`+string(origin)+`"`)
		}
		members = append(members, string(directive.Feature)+"=("+strings.Join(items, " ")+")")
	}
	return HeaderValue(strings.Join(members, ", ")), nil
}

func validPermissionFeature(feature PermissionFeature) bool {
	if feature == "" || len(feature) > 64 || feature[0] < 'a' || feature[0] > 'z' {
		return false
	}
	for i := range len(feature) {
		c := feature[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
}
