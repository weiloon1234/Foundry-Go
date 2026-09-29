package oauth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Profile is the verified identity a provider asserted. Subject is the stable
// account identifier at the provider (the OpenID sub claim or GitHub's numeric
// account ID) and is the key to link on; email addresses can change and are
// only trustworthy when EmailVerified. Mapping a profile to the application's
// own user, including creating or linking one, is the application's decision.
type Profile struct {
	Provider      ProviderName
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	// Username is the provider handle when it has one (GitHub login).
	Username  string
	AvatarURL string
	// Token is the provider access for further API calls; it is redacted.
	Token Token
}

// Token is the provider's credential for calling its APIs on the user's behalf.
// Formatting, logging and JSON redact it; Access and Refresh are explicit
// disclosure boundaries.
type Token struct {
	Access    secret.String
	Refresh   value.Optional[secret.String]
	ExpiresAt value.Optional[time.Time]
	Scopes    []string
}

func (Token) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Token) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Token) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// HasScope reports whether the provider reported granting scope.
func (t Token) HasScope(scope string) bool { return slices.Contains(t.Scopes, scope) }
