package coremodels

import (
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/value"
)

type EmailInput struct{ Address string }
type StoredEmail string
type EmailLink string
type LabelInput struct{ Text string }
type StoredLabel string
type DisplayLabel string
type BinaryInput []byte
type StoredBody []byte

var MutatorVeto = errors.New("mutator fixture veto")

func NormalizeEmail(v EmailInput) (StoredEmail, error) {
	if strings.HasPrefix(v.Address, "reject:") {
		return "", &MutatorInputError{Value: v.Address}
	}
	if v.Address == "reject" {
		return "", MutatorVeto
	}
	return StoredEmail(strings.ToLower(strings.TrimSpace(v.Address))), nil
}
func NormalizeLabel(v LabelInput) (StoredLabel, error) {
	return StoredLabel(strings.TrimSpace(v.Text)), nil
}
func NormalizeName(v string) (string, error)          { return strings.TrimSpace(v), nil }
func NormalizeBody(v BinaryInput) (StoredBody, error) { return StoredBody(v), nil }
func DisplayNote(v value.Nullable[StoredLabel]) (value.Nullable[DisplayLabel], error) {
	x, ok := v.Get()
	if !ok {
		return value.Null[DisplayLabel](), nil
	}
	return value.Of(DisplayLabel(x)), nil
}

// MutatorInputError deliberately includes private input to exercise redacted wrappers.
type MutatorInputError struct{ Value string }

func (e *MutatorInputError) Error() string { return "invalid email: " + e.Value }
