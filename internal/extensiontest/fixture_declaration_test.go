package extensiontest

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestMemberDefaultAndComputedDeclarations(t *testing.T) {
	if err := orm.Check(memberFields, memberSource("extension_members")); err != nil {
		t.Fatal(err)
	}
	generated := orm.IdentityOf(memberFields.Table, memberFields.Key())
	computed := MemberIdentity("extension_members")
	generatedKey, err := generated.KeyToken(42)
	if err != nil {
		t.Fatal(err)
	}
	computedKey, err := computed.KeyToken(42)
	if err != nil {
		t.Fatal(err)
	}
	if generated.ModelName() != computed.ModelName() || generatedKey != computedKey {
		t.Fatal("default and computed identities differ")
	}
	identity := MemberIdentity("renamed_members")
	renamed := identity.ModelName()
	err = identity.Validate()
	if err != nil || renamed != "renamed_members" {
		t.Fatalf("computed identity: %q %v", renamed, err)
	}
}
