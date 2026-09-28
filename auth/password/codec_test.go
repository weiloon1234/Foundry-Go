package password

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func codecHash() Hash {
	return encodedHash(DefaultConfig().Parameters, []byte("1234567890123456"), []byte("12345678901234567890123456789012"))
}
func TestHashCodecOwnsPHCPersistenceAndDisclosureMetadata(t *testing.T) {
	hash := codecHash()
	c := Codec()
	if !c.SensitiveValues() || c.ParameterType() != codec.TypeText {
		t.Fatal("missing sensitive text metadata")
	}
	raw, err := c.Bind(hash)
	if err != nil || raw != hash.Encoded().Reveal() {
		t.Fatal("wrong persistence value", err)
	}
	for _, input := range []any{raw, []byte(raw.(string))} {
		got, err := c.Decode(input)
		if err != nil || got != hash {
			t.Fatal("typed round trip", err)
		}
	}
	var valuer driver.Valuer = hash
	if got, err := valuer.Value(); err != nil || got != raw {
		t.Fatal("sql.Value diverged from codec", err)
	}
	buffer := []byte(raw.(string))
	var scanned Hash
	if err := scanned.Scan(buffer); err != nil {
		t.Fatal(err)
	}
	clear(buffer)
	if scanned != hash {
		t.Fatal("driver bytes were retained")
	}
	for _, input := range []any{nil, "not PHC", int64(7), []byte{0}} {
		got, err := c.Decode(input)
		if !errors.Is(err, fault.Invalid) || !got.Encoded().IsZero() {
			t.Fatal("invalid database hash accepted", err)
		}
		if err := scanned.Scan(input); err == nil || scanned != hash {
			t.Fatal("failed scan changed destination")
		}
	}
	if _, err := c.Bind(Hash{}); err == nil {
		t.Fatal("zero hash bound")
	}
	var missing *Hash
	if err := missing.Scan(raw); err == nil {
		t.Fatal("nil scan accepted")
	}
	nullable := codec.Nullable(c)
	if !nullable.SensitiveValues() {
		t.Fatal("nullable lost sensitivity")
	}
	if bound, err := nullable.Bind(value.Null[Hash]()); err != nil || bound != nil {
		t.Fatal("nullable binding", err)
	}
	decoded, err := nullable.Decode(nil)
	_, present := decoded.Get()
	if err != nil || present {
		t.Fatal("nullable hydration", err)
	}
	reference := model.NewReference[struct{}]("invalid_hash_identity", hash, c)
	if reference.Validate() == nil {
		t.Fatal("sensitive identity declaration accepted")
	}
	if identity, err := reference.Identity(); err == nil || !identity.IsZero() {
		t.Fatal("hash exported as identity")
	}
}
