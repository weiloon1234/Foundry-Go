package mfa

import (
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// ParseEnrollmentID parses a nonzero enrollment identifier retaining model ownership.
func ParseEnrollmentID[M any](text string) (EnrollmentID[M], error) {
	id, err := model.ParseID[Record](text)
	if err != nil {
		return EnrollmentID[M]{}, err
	}
	if id.IsZero() {
		return EnrollmentID[M]{}, safeerror.NewFault(fault.Invalid, "enrollment ID is empty")
	}
	return EnrollmentID[M]{id: id}, nil
}

// String returns the enrollment identifier's text representation.
func (id EnrollmentID[M]) String() string { return id.id.String() }

// IsZero reports whether the enrollment identifier is uninitialized.
func (id EnrollmentID[M]) IsZero() bool { return id.id.IsZero() }

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

// Subject returns the enrollment's model snapshot without cloning it.
func (e Enrollment[M]) Subject() M { return e.subject }

// ID returns the model-owned enrollment identifier.
func (e Enrollment[M]) ID() EnrollmentID[M] { return e.id }

// Secret exposes the enrollment's TOTP key for authenticated delivery.
func (e Enrollment[M]) Secret() TOTPSecret { return e.key }

// URI exposes the secret provisioning URI for authenticated delivery.
func (e Enrollment[M]) URI() secret.String { return e.uri }

// CreatedAt returns the enrollment's creation time.
func (e Enrollment[M]) CreatedAt() temporal.DateTime { return e.created }

// ExpiresAt returns the pending enrollment's confirmation deadline.
func (e Enrollment[M]) ExpiresAt() temporal.DateTime { return e.expires }

// Format writes the label "MFA enrollment" without exposing the value.
func (Enrollment[M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("MFA enrollment")) }

// RecoveryCodes exposes an owned slice only after the factor mutation commits.
// Ordinary serialization cannot disclose the private collection or model.
type RecoveryCodes[M any] struct {
	subject M
	id      EnrollmentID[M]
	codes   []RecoveryCode
}

// Subject returns the model snapshot associated with code issuance without cloning it.
func (r RecoveryCodes[M]) Subject() M { return r.subject }

// ID returns the model-owned enrollment identifier associated with the codes.
func (r RecoveryCodes[M]) ID() EnrollmentID[M] { return r.id }

// Codes returns a copy of the issued recovery-code slice for explicit delivery.
func (r RecoveryCodes[M]) Codes() []RecoveryCode { return slices.Clone(r.codes) }

// Format writes the label "issued MFA recovery codes" without exposing the value.
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

// TOTPResponse validates code and selects TOTP verification.
func TOTPResponse(code TOTPCode) (Response, error) {
	if err := code.Validate(); err != nil {
		return Response{}, err
	}
	return Response{kind: 1, code: code}, nil
}

// RecoveryResponse validates code and selects recovery-code verification.
func RecoveryResponse(code RecoveryCode) (Response, error) {
	if err := code.Validate(); err != nil {
		return Response{}, err
	}
	return Response{kind: 2, recovery: code}, nil
}

// Format writes the redaction marker without exposing the value.
func (Response) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(secret.Redacted)) }

// Validate requires exactly one supported factor response with valid syntax.
func (r Response) Validate() error {
	switch r.kind {
	case 1:
		return r.code.Validate()
	case 2:
		return r.recovery.Validate()
	default:
		return safeerror.NewFault(fault.Invalid, "MFA response must select a factor")
	}
}
