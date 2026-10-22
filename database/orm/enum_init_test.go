package orm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/internal/enuminit"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestEnumsInitializeAfterModelDeclaration(t *testing.T) {
	for index := range 3 {
		as := []orm.Assignment[enuminit.Model]{enuminit.Models.Key().Set(enuminit.Active), enuminit.Models.State.Set(enuminit.Active), enuminit.Models.Nullable.Set(enuminit.Active)}
		bad := enuminit.Status("unknown")
		switch index {
		case 0:
			as[0] = enuminit.Models.Key().Set(bad)
		case 1:
			as[1] = enuminit.Models.State.Set(bad)
		case 2:
			as[2] = enuminit.Models.Nullable.Set(bad)
		}
		if _, err := enuminit.Models.Insert(t.Context(), refusingWriter{}, as...); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if err := orm.Check(enuminit.Models); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(enuminit.Rows); err != nil {
		t.Fatal(err)
	}

}

type refusingWriter struct{}

func (refusingWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	panic("invalid enum opened a transaction")
}
