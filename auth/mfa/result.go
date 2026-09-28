package mfa

import (
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// EnrollmentID preserves the model type. Parsing identifies a pending factor;
// it does not authenticate or authorize confirmation.
type EnrollmentID[M any] struct {
	_  [0]*M
	id model.ID[Record]
}

func ParseEnrollmentID[M any](text string) (EnrollmentID[M], error) {
	id, err := model.ParseID[Record](text)
	if err != nil {
		return EnrollmentID[M]{}, err
	}
	if id.IsZero() {
		return EnrollmentID[M]{}, fault.New(fault.Invalid, "enrollment ID is empty")
	}
	return EnrollmentID[M]{id: id}, nil
}
func (id EnrollmentID[M]) String() string { return id.id.String() }
func (id EnrollmentID[M]) IsZero() bool   { return id.id.IsZero() }

// Enrollment is returned only after the pending encrypted factor commits.
// Secret and URI are explicit disclosure boundaries for the enrolled account.
type Enrollment[M any] struct {
	subject M
	id      EnrollmentID[M]
	key     TOTPSecret
	uri     secret.String
	created temporal.DateTime
	expires temporal.DateTime
}

func (e Enrollment[M]) Subject() M                   { return e.subject }
func (e Enrollment[M]) ID() EnrollmentID[M]          { return e.id }
func (e Enrollment[M]) Secret() TOTPSecret           { return e.key }
func (e Enrollment[M]) URI() secret.String           { return e.uri }
func (e Enrollment[M]) CreatedAt() temporal.DateTime { return e.created }
func (e Enrollment[M]) ExpiresAt() temporal.DateTime { return e.expires }
func (Enrollment[M]) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("MFA enrollment")) }

// RecoveryCodes exposes an owned slice only after the factor mutation commits.
// Ordinary serialization cannot disclose the private collection or model.
type RecoveryCodes[M any] struct {
	subject M
	id      EnrollmentID[M]
	codes   []RecoveryCode
}

func (r RecoveryCodes[M]) Subject() M            { return r.subject }
func (r RecoveryCodes[M]) ID() EnrollmentID[M]   { return r.id }
func (r RecoveryCodes[M]) Codes() []RecoveryCode { return slices.Clone(r.codes) }
func (RecoveryCodes[M]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("issued MFA recovery codes"))
}

// Response is an explicit choice of TOTP or recovery factor. Enrollment
// confirmation accepts only TOTPCode and cannot receive this alternative.
type Response struct {
	kind     uint8
	code     TOTPCode
	recovery RecoveryCode
}

func TOTPResponse(code TOTPCode) (Response, error) {
	if err := code.Validate(); err != nil {
		return Response{}, err
	}
	return Response{kind: 1, code: code}, nil
}
func RecoveryResponse(code RecoveryCode) (Response, error) {
	if err := code.Validate(); err != nil {
		return Response{}, err
	}
	return Response{kind: 2, recovery: code}, nil
}
func (Response) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(secret.Redacted)) }
func (r Response) Validate() error {
	switch r.kind {
	case 1:
		return r.code.Validate()
	case 2:
		return r.recovery.Validate()
	default:
		return fault.New(fault.Invalid, "MFA response must select a factor")
	}
}
