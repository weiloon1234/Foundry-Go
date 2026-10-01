package generate

import (
	"strings"
	"testing"
)

func TestEncryptedFieldsGenerateSealingAndOpening(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/database/encrypted";"github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/value")
type Settings struct{Region string `+"`json:\"region\"`"+`}
//foundry:model table=vault_entries
type Entry struct{ID model.ID[Entry];Token encrypted.Text;Settings value.Nullable[encrypted.JSON[Settings]]}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["entry_foundry.gen.go"]
	for _, want := range []string{
		`Name: "token", Nullable: false, DatabaseDefault: false, Encrypted: true}`,
		`OpenEncrypted(row, "vault_entries", "token"`,
		`OpenNullableEncrypted(row, "vault_entries", "settings"`,
		`AssignEncrypted[Entry]("vault_entries", "token", encrypted.TextCodec(), v)`,
		`AssignNullableEncrypted[Entry]("vault_entries", "settings", codec.Nullable(encrypted.JSONCodec[Settings]()), v)`,
		`EncryptedField[FoundryScope, encrypted.Text]`,
		"Encrypted with the database key ring",
	} {
		if !strings.Contains(output, want) {
			t.Fatal("encrypted model omitted", want)
		}
	}
	// Encrypted envelopes are bound to one row, so SQL copies are not offered.
	if strings.Contains(output, "SelectToken(") || strings.Contains(output, "SelectSettings(") {
		t.Fatal("encrypted fields offered SQL source mappings")
	}
}

func TestEncryptedFieldsRejectUnboundableDeclarations(t *testing.T) {
	for name, test := range map[string]struct{ source, message string }{
		"identity": {`//foundry:model table=bad primary=Token
type Bad struct{Token encrypted.Text}`, "encrypted fields cannot be model identity keys"},
		"database key": {`//foundry:model table=bad
type Bad struct{ID model.ID[Bad] ` + "`foundry:\"default=database\"`" + `;Token encrypted.Text}`, "encrypted fields require an application-assigned primary key"},
		"database default": {`//foundry:model table=bad
type Bad struct{ID model.ID[Bad];Token encrypted.Text ` + "`foundry:\"default=database\"`" + `}`, "encrypted fields cannot use database defaults"},
		"mutator": {`//foundry:model table=bad
type Bad struct{ID model.ID[Bad];Token encrypted.Text}
func (Bad) MutateToken(v encrypted.Text) (encrypted.Text, error) { return v, nil }`, "encrypted fields cannot declare mutators"},
		"projection": {`//foundry:projection
type Bad struct{Token encrypted.Text}`, "projections cannot select encrypted fields"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/database/encrypted";"github.com/weiloon1234/Foundry-Go/model")
var _ model.Identity
`+test.source+"\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("%s accepted: %v", name, err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid encrypted declaration published output")
			}
		})
	}
}
