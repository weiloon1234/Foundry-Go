package consumer_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerCodecsReuseEnumRulesAndPreserveNamedTypes(t *testing.T) {
	status := models.StatusCodec()
	if _, err := status.Bind(models.Status("not-declared")); !errors.Is(err, fault.Invalid) {
		t.Fatal("persistence skipped generated enum validation")
	}
	if _, err := codec.Nullable(status).Decode("not-declared"); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable persistence skipped generated enum validation")
	}
	if decoded, err := codec.Nullable(status).Decode(nil); err != nil || decoded != value.Null[models.Status]() {
		t.Fatal("SQL NULL was confused with an invalid enum member")
	}
	var country models.CountryCode
	if err := codec.String[models.CountryCode]().Scan(&country).Scan("MY"); err != nil || country != models.CountryCode("MY") {
		t.Fatal("natural key type did not survive scanning")
	}
	amount, err := decimal.Parse("9007199254740993.100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	inspectInsertAssignments(t, models.LedgerEntries.Table, []orm.Assignment[models.LedgerEntry]{orm.NumberSet(models.LedgerEntries.Amount, amount)}, func(write *orm.Write[models.LedgerEntry]) {
		if v, ok := orm.WriteGet(write, func() func(arg0 decimal.Decimal) orm.Assignment[models.LedgerEntry] {
			receiver := models.LedgerEntries.Amount
			return func(arg0 decimal.Decimal) orm.Assignment[models.LedgerEntry] { return orm.NumberSet(receiver, arg0) }
		}()).Get(); !ok || v != amount {
			t.Error("decimal assignment changed exact value")
		}
	})
	statement, err := orm.Compile(models.LedgerEntries.Query().Where(orm.NumberGte(models.LedgerEntries.Amount, amount), orm.NullableNumberIsNull(models.LedgerEntries.Balance)))
	if err != nil {
		t.Fatal(err)
	}
	if args := statement.Arguments(); len(args) != 1 || args[0] != amount.String() {
		t.Fatal("decimal binding changed exact value")
	}
}
