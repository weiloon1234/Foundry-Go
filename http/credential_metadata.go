package http

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type CredentialKind string

const authorizationHeader = "Authorization"

const (
	BearerCredentialKind CredentialKind = "bearer"
	CookieCredentialKind CredentialKind = "cookie"
)

// CredentialInfo describes where the selected guard reads a credential. It
// contains only public transport names, never tokens or cookie values. Browser
// session cookies also expose their required native origin/CSRF protection.
type CredentialInfo struct {
	Source           auth.CredentialName `json:"source"`
	Kind             CredentialKind      `json:"kind"`
	Name             string              `json:"name"`
	OriginProtection bool                `json:"origin_protection,omitempty"`
}

func (info CredentialInfo) Validate() error {
	if !identifier.Semantic(string(info.Source)) {
		return fault.New(fault.Invalid, "credential metadata requires a source")
	}
	switch info.Kind {
	case BearerCredentialKind:
		if info.Name == authorizationHeader && !info.OriginProtection {
			return nil
		}
	case CookieCredentialKind:
		return CookieName(info.Name).Validate()
	}
	return fault.New(fault.Invalid, "unsupported credential transport metadata")
}
