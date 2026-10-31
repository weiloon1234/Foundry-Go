package token

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ID is a model-owned token family identifier, never an authentication secret.
// It remains stable across refresh. Explicit subject authorization is still needed.
type ID[M any] struct {
	_     [0]*M
	value model.ID[Record]
}

// String returns the token identifier's text representation.
func (id ID[M]) String() string { return id.value.String() }

// IsZero reports whether the identifier is uninitialized.
func (id ID[M]) IsZero() bool { return id.value.IsZero() }

// MarshalText encodes the token identifier in text form.
func (id ID[M]) MarshalText() ([]byte, error) { return id.value.MarshalText() }

// ParseID parses a token identifier while retaining model ownership.
func ParseID[M any](text string) (ID[M], error) {
	id, err := model.ParseID[Record](text)
	return ID[M]{value: id}, err
}

// Info exposes immutable metadata and a concrete stored model reference.
// It contains neither raw credentials nor hashes and does not authorize its caller.
type Info[M, K any] struct {
	id                                                ID[M]
	subject                                           model.Reference[M, K]
	name                                              string
	scopes                                            auth.AccessScopes[M]
	mode                                              Mode
	assurance                                         auth.Assurance
	generation                                        uint32
	created, issued, lastSeen, accessExpires, expires temporal.DateTime
	refreshExpires                                    value.Optional[temporal.DateTime]
	device                                            auth.Device
}

// Device is the client metadata captured when the token family was issued.
func (i Info[M, K]) Device() auth.Device { return i.device }

// ID returns the model-owned token identifier.
func (i Info[M, K]) ID() ID[M] { return i.id }

// Subject returns the token's typed model reference.
func (i Info[M, K]) Subject() model.Reference[M, K] { return i.subject }

// Name returns the token's assigned display name.
func (i Info[M, K]) Name() string { return i.name }

// Scopes returns the token's immutable model-owned capability grants.
func (i Info[M, K]) Scopes() auth.AccessScopes[M] { return i.scopes }

// Mode returns the token's personal, renewable or challenge mode.
func (i Info[M, K]) Mode() Mode { return i.mode }

// Assurance returns the token's authentication assurance.
func (i Info[M, K]) Assurance() auth.Assurance { return i.assurance }

// Generation returns the token family's rotation generation.
func (i Info[M, K]) Generation() uint32 { return i.generation }

// CreatedAt returns the token's creation time.
func (i Info[M, K]) CreatedAt() temporal.DateTime { return i.created }

// IssuedAt returns this generation's issue time.
func (i Info[M, K]) IssuedAt() temporal.DateTime { return i.issued }

// LastSeenAt returns the token's recorded activity time.
func (i Info[M, K]) LastSeenAt() temporal.DateTime { return i.lastSeen }

// AccessExpiresAt returns the access credential's expiry.
func (i Info[M, K]) AccessExpiresAt() temporal.DateTime { return i.accessExpires }

// RefreshExpiresAt returns the optional refresh credential deadline.
func (i Info[M, K]) RefreshExpiresAt() value.Optional[temporal.DateTime] { return i.refreshExpires }

// ExpiresAt returns the token's absolute expiry.
func (i Info[M, K]) ExpiresAt() temporal.DateTime { return i.expires }

// Format writes the label "token metadata" without exposing the value.
func (Info[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token metadata")) }

// Issued retains raw credentials for explicit transport delivery only. Routine
// formatting and JSON do not disclose them. Nonrenewable tokens omit refresh.
type Issued[M, K any] struct {
	info    Info[M, K]
	access  secret.String
	refresh value.Optional[secret.String]
}

// Info returns the issued token's safe metadata.
func (i Issued[M, K]) Info() Info[M, K] { return i.info }

// AccessSecret explicitly exposes the issued access secret wrapper for delivery.
func (i Issued[M, K]) AccessSecret() secret.String { return i.access }

// RefreshSecret returns the optional refresh secret wrapper for explicit delivery.
func (i Issued[M, K]) RefreshSecret() value.Optional[secret.String] { return i.refresh }

// Format writes the label "issued token" without exposing the value.
func (Issued[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("issued token")) }
