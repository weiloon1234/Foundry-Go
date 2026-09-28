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

func (id ID[M]) String() string               { return id.value.String() }
func (id ID[M]) IsZero() bool                 { return id.value.IsZero() }
func (id ID[M]) MarshalText() ([]byte, error) { return id.value.MarshalText() }
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
}

func (i Info[M, K]) ID() ID[M]                                           { return i.id }
func (i Info[M, K]) Subject() model.Reference[M, K]                      { return i.subject }
func (i Info[M, K]) Name() string                                        { return i.name }
func (i Info[M, K]) Scopes() auth.AccessScopes[M]                        { return i.scopes }
func (i Info[M, K]) Mode() Mode                                          { return i.mode }
func (i Info[M, K]) Assurance() auth.Assurance                           { return i.assurance }
func (i Info[M, K]) Generation() uint32                                  { return i.generation }
func (i Info[M, K]) CreatedAt() temporal.DateTime                        { return i.created }
func (i Info[M, K]) IssuedAt() temporal.DateTime                         { return i.issued }
func (i Info[M, K]) LastSeenAt() temporal.DateTime                       { return i.lastSeen }
func (i Info[M, K]) AccessExpiresAt() temporal.DateTime                  { return i.accessExpires }
func (i Info[M, K]) RefreshExpiresAt() value.Optional[temporal.DateTime] { return i.refreshExpires }
func (i Info[M, K]) ExpiresAt() temporal.DateTime                        { return i.expires }
func (Info[M, K]) Format(s fmt.State, _ rune)                            { _, _ = s.Write([]byte("token metadata")) }

// Issued retains raw credentials for explicit transport delivery only. Routine
// formatting and JSON do not disclose them. Nonrenewable tokens omit refresh.
type Issued[M, K any] struct {
	info    Info[M, K]
	access  secret.String
	refresh value.Optional[secret.String]
}

func (i Issued[M, K]) Info() Info[M, K]                             { return i.info }
func (i Issued[M, K]) AccessSecret() secret.String                  { return i.access }
func (i Issued[M, K]) RefreshSecret() value.Optional[secret.String] { return i.refresh }
func (Issued[M, K]) Format(s fmt.State, _ rune)                     { _, _ = s.Write([]byte("issued token")) }
