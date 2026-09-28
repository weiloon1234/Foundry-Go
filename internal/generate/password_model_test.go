package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordHashFieldsUseTypedSensitiveCodec(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/auth/password";"github.com/weiloon1234/Foundry-Go/value")
type StoredHash = password.Hash
//foundry:model table=accounts primary=ID
type Account struct{ID int64;Digest StoredHash;Backup value.Nullable[password.Hash]}
//foundry:projection
type Credential struct{Digest StoredHash}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["account_foundry.gen.go"]
	for _, want := range []string{"password.Codec()", "SetDigest(v StoredHash)", "ScalarField[FoundryScope, StoredHash]", "Nullable(password.Codec())"} {
		if !strings.Contains(output, want) {
			t.Fatal("password model omitted typed contract", want)
		}
	}
	source, err := os.ReadFile(filepath.Join(dir, "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "Sensitive stored password hash") || !strings.Contains(output, "Sensitive stored password hash") {
		t.Fatal("missing automatic field notice")
	}
}
func TestPasswordHashCannotBeGeneratedIdentity(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/auth/password"
//foundry:model table=bad_accounts primary=Digest
type Account struct{Digest password.Hash}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "password hashes cannot be model identity keys") {
		t.Fatal("sensitive primary key accepted", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid hash identity published output")
	}
}
