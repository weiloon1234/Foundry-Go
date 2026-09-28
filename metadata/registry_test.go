package metadata

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestMetadataDeclarationsRejectInvalidSchemasAndDuplicateKeys(t *testing.T) {
	owner := extensiontest.Members
	for _, key := range []Key[extensiontest.Member, int64, string]{Define(owner, "bad name", 1, contract.StringJSON[string]()), Define(owner, "theme", 0, contract.StringJSON[string]()), Define(owner, "theme", 1, contract.JSON[string]{})} {
		if key.Validate() == nil {
			t.Fatal("invalid metadata declaration")
		}
	}
	owners, err := extensions.NewRegistry(owner.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extensions.NewRegistry(owner.Registration(), owner.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate owners", err)
	}
	key := Define(owner, "theme", 1, contract.StringJSON[string]())
	if err := key.Registration().validate(owners); err != nil {
		t.Fatal(err)
	}
	if err := Define(extensiontest.Others, "theme", 1, contract.StringJSON[string]()).Registration().validate(owners); err == nil {
		t.Fatal("unregistered owner accepted")
	}
}
