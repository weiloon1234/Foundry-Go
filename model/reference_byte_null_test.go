package model_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// This implements the narrow public key interface directly, ensuring references
// validate NULL semantics even when the caller bypasses codec.Codec.Bind.
type byteReferenceCodec struct{}

func (byteReferenceCodec) Bind(v []byte) (driver.Value, error) { return v, nil }
func (byteReferenceCodec) Decode(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return v.([]byte), nil
}

func TestReferenceRejectsNilByteSQLNullAndKeepsEmptyKey(t *testing.T) {
	if _, err := model.NewReference[referencedMember]("members", []byte(nil), byteReferenceCodec{}).Identity(); !errors.Is(err, fault.Invalid) {
		t.Fatal("SQL NULL became a model identity", err)
	}
	ref := model.NewReference[referencedMember]("members", []byte{}, byteReferenceCodec{})
	identity, err := ref.Identity()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ref.Parse(identity)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Key() == nil || len(restored.Key()) != 0 {
		t.Fatal("non-null empty key changed")
	}
}
