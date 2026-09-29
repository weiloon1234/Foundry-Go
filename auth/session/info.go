package session

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ID is a public session identifier owned by model M. It is not an authentication
// secret or a model primary key, and remains stable across session rotation.
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

// Info exposes session metadata with a concrete stored subject reference. It
// carries no secret/hash and does not authorize the caller to act as that subject.
type Info[M, K any] struct {
	id                                      ID[M]
	subject                                 model.Reference[M, K]
	assurance                               auth.Assurance
	remembered                              bool
	created, lastSeen, idleExpires, expires temporal.DateTime
	device                                  auth.Device
	confirmed                               value.Optional[temporal.DateTime]
	impersonator                            value.Optional[Impersonator]
}

func (i Info[M, K]) ID() ID[M]                        { return i.id }
func (i Info[M, K]) Subject() model.Reference[M, K]   { return i.subject }
func (i Info[M, K]) Assurance() auth.Assurance        { return i.assurance }
func (i Info[M, K]) Remembered() bool                 { return i.remembered }
func (i Info[M, K]) CreatedAt() temporal.DateTime     { return i.created }
func (i Info[M, K]) LastSeenAt() temporal.DateTime    { return i.lastSeen }
func (i Info[M, K]) IdleExpiresAt() temporal.DateTime { return i.idleExpires }
func (i Info[M, K]) ExpiresAt() temporal.DateTime     { return i.expires }

// Device is the client metadata captured when the session was issued.
func (i Info[M, K]) Device() auth.Device { return i.device }

// ConfirmedAt is when the holder last re-entered its password in this session.
func (i Info[M, K]) ConfirmedAt() value.Optional[temporal.DateTime] { return i.confirmed }

// Impersonator is the original actor when this is an impersonation session.
func (i Info[M, K]) Impersonator() value.Optional[Impersonator] { return i.impersonator }

// Impersonated reports whether this session was started by impersonation.
func (i Info[M, K]) Impersonated() bool       { return i.impersonator.IsSet() }
func (Info[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("session metadata")) }

// Issued owns the newly generated secret. Only Secret exposes it for the explicit
// transport boundary; routine formatting and JSON never serialize credentials.
// Losing the response may leave an orphan session until expiry or revocation.
type Issued[M, K any] struct {
	info   Info[M, K]
	secret secret.String
}

func (i Issued[M, K]) Info() Info[M, K]         { return i.info }
func (i Issued[M, K]) Secret() secret.String    { return i.secret }
func (Issued[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("issued session")) }
