package consumer_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
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
	draft := models.LedgerEntryDraft{}.SetAmount(amount).ClearBalance()
	if pending, ok := draft.Amount().Get(); !ok || pending != amount {
		t.Fatal("generated decimal mutation changed exact value")
	}
	if err := models.QueryLedgerEntries().Where(models.LedgerEntryFields().Amount.Gte(amount), models.LedgerEntryFields().Balance.IsNull()).Validate(); err != nil {
		t.Fatal(err)
	}
}
