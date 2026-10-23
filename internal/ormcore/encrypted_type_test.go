package ormcore

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/value"
)

type wrappedEncryption struct{ encrypted.Text }
type encryptionLookalike struct{}

func (encryptionLookalike) Seal()                          {}
func (encryptionLookalike) Open()                          {}
func (encryptionLookalike) Equal(encryptionLookalike) bool { return true }

func TestEncryptedType(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[encrypted.Value[encrypted.Text]](), reflect.TypeFor[wrappedEncryption](), reflect.TypeFor[encrypted.Text](), reflect.TypeFor[encrypted.JSON[struct{ Text string }]](),
		reflect.TypeFor[value.Nullable[encrypted.Text]](), reflect.TypeFor[value.Nullable[value.Nullable[encrypted.Text]]](),
	} {
		if !EncryptedType(typ) {
			t.Fatalf("missed encrypted type %v", typ)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[value.Nullable[string]](), reflect.TypeFor[encryptionLookalike]()} {
		if EncryptedType(typ) {
			t.Fatalf("misidentified %v", typ)
		}
	}
}
