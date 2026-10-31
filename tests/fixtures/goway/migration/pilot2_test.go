package migration_test

import (
	"testing"

	ext "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// migration:pilot2-boolean-enum
type CountryFlag struct {
	Status  countries.Status
	Enabled bool
}
type CountryFlagColumns struct {
	rows.Row[CountryFlag]
	Status  rows.Field[CountryFlag, countries.Status]
	Enabled rows.Field[CountryFlag, bool]
}

var CountryFlags = rows.Define(func(b rows.Builder[CountryFlag], r *CountryFlag) CountryFlagColumns {
	return CountryFlagColumns{b.Row(), rows.EnumOf(b, &r.Status), rows.BoolOf(b, &r.Enabled)}
})

// migration:end

// migration:pilot2-row-constant
func enabledAdmins() rows.Scalar[Admin, bool] {
	enabled := expr.When(expr.Eq(expr.Value(orm.TextExpr(Admins.Username)), "active"),
		expr.Constant[Admin](codec.Bool[bool](), true)).Else(expr.Constant[Admin](codec.Bool[bool](), false))
	return rows.SelectScalar(rows.From(Admins.Query()), expr.AsSelected(enabled))
}
func scopedUsername() {
	type adminScope struct{}
	source := rows.As[adminScope](Admins.Query(), "admin")
	parameter := expr.ParamOf(expr.Row(expr.At(rows.Scope(source), orm.TextExpr(Admins.Username))), "Ada")
	_ = parameter
}

// migration:end

// migration:pilot2-json-startup
func validateJSONDeclarations() error {
	// JSONFieldOf already resolved the selector; surface its stored error at startup.
	// orm.Check validates tables, not standalone JSON field bindings.
	return Theme.Validate()
}

// migration:end

// migration:pilot2-slot-name
// The Go slot is Gallery; the persisted collection remains "public-media".
// Bind the field with orm.SlotOf in the model declaration, without a foundry tag.
var PublicMedia = attachments.ManyOf(ext.Owner, "public-media",
	attachments.Policy{Disk: storage.DefineDisk("local"), Accepted: []storage.MediaType{"image/png"}},
	extensions.BindSlot(ext.Articles.Key(), ext.Articles.Gallery))

// migration:end

func TestPilot2Declarations(t *testing.T) {
	if err := orm.Check(Admins.Table); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(CountryFlags); err != nil {
		t.Fatal(err)
	}
	if err := validateJSONDeclarations(); err != nil {
		t.Fatal(err)
	}
	if err := PublicMedia.Validate(); err != nil {
		t.Fatal(err)
	}
	if PublicMedia.Describe().Name != "public-media" || PublicMedia.Describe().Field != "Gallery" {
		t.Fatal("stored identity followed Go field name")
	}
	scopedUsername()
	_ = enabledAdmins()
}
