package vault_test

import (
	"testing"

	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(vault.Entries.Table); err != nil {
		t.Fatal(err)
	}
}
