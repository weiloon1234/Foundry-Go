package extensiontest

import (
	"bytes"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestMemberDefaultAndComputedDeclarations(t *testing.T) {
	before, err := orm.DeclarationSnapshot(memberSource("extension_members"), "members", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Compare the pre-existing handles; generation also exposes the Soft token.
	after, err := orm.DeclarationSnapshot(memberTable{memberFields.Table, memberFields.DeletedAt}, "members", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("default declaration changed")
	}
	identity := MemberIdentity("renamed_members")
	renamed := identity.ModelName()
	err = identity.Validate()
	if err != nil || renamed != "renamed_members" {
		t.Fatalf("computed identity: %q %v", renamed, err)
	}
}
