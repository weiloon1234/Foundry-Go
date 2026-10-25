package recovering_test

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"
)

func TestORMDeclaration(t *testing.T) {
	if err := orm.Check(recovering.MemberRecords.Table); err != nil {
		t.Fatal(err)
	}
	if err := recovering.MemberIdentity.Validate(); err != nil {
		t.Fatal(err)
	}
}
