package consumer_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ driver.Valuer = models.StatusActive
var _ sql.Scanner = (*models.Status)(nil)

func TestGeneratedEnumPersistenceAndDescriptor(t *testing.T) {
	status := models.StatusActive
	if err := status.Scan([]byte("disabled")); err != nil || status != models.StatusDisabled {
		t.Fatalf("scan = %v, %v", status, err)
	}
	for _, input := range []any{nil, int64(1), "invalid", true} {
		if err := status.Scan(input); err == nil || status != models.StatusDisabled {
			t.Fatalf("accepted invalid scan: %T", input)
		}
	}
	if db, err := status.Value(); err != nil || db != "disabled" {
		t.Fatalf("database value = %v, %v", db, err)
	}
	level := models.LevelBasic
	for _, input := range []any{int64(2), "2", []byte("2")} {
		if err := level.Scan(input); err != nil || level != models.LevelAdvanced {
			t.Fatalf("integer scan = %v, %v", level, err)
		}
	}
	for _, input := range []any{int64(258), int64(-1), float64(2), nil} {
		if err := level.Scan(input); err == nil || level != models.LevelAdvanced {
			t.Fatal("integer scan bypassed validation")
		}
	}
	definition, err := status.EnumDescriptor().Definition()
	if err != nil {
		t.Fatal(err)
	}
	if definition.PackagePath != "foundry.test/consumer/models" || definition.Name != "Status" || len(definition.Cases) != 2 || string(definition.Cases[0].Value) != `"active"` {
		t.Fatalf("definition = %+v", definition)
	}
}

func TestGeneratedModelsQueriesAndDrafts(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	fields := models.UserFields()
	q := models.QueryUsers().Where(fields.ID.Eq(id), query.Or(fields.Email.Contains("@example.test"), fields.Nickname.IsNull()), fields.Age.Gte(18), fields.Status.Eq(models.StatusActive)).OrderBy(fields.Age.Desc())
	if err := q.Validate(); err != nil || q.Table() != "users" {
		t.Fatalf("query declaration: %v", err)
	}
	if err := models.QueryCountries().Where(models.CountryFields().Code.Eq(models.CountryCode("MY"))).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := models.QueryUsers().Where(fields.IntroducerID.Eq(id)).Validate(); err != nil {
		t.Fatal("self-reference lost its owner")
	}
	base := models.UserDraft{}
	changed := base.SetID(id).SetAge(0).SetEmail("").ClearNickname().SetIntroducerID(id)
	if !base.IsEmpty() || changed.IsEmpty() {
		t.Fatal("draft derivation mutated original")
	}
	if age, set := changed.Age().Get(); !set || age != 0 {
		t.Fatal("explicit zero lost")
	}
	if nickname, set := changed.Nickname().Get(); !set || !nickname.IsNull() {
		t.Fatal("explicit null lost")
	}
	if changed.UnsetNickname().Nickname().IsSet() {
		t.Fatal("unset became null")
	}
	if email, set := (models.User{Email: "typed"}).CurrentEmailDraft().Email().Get(); !set || email != "typed" {
		t.Fatal("handwritten business method lost generated type")
	}
}

func TestGeneratedEnumBoundaries(t *testing.T) {
	if !reflect.DeepEqual(models.StatusValues(), []models.Status{models.StatusActive, models.StatusDisabled}) {
		t.Fatal("enum declaration order lost")
	}
	copy := models.StatusValues()
	copy[0] = models.StatusDisabled
	if models.StatusValues()[0] != models.StatusActive {
		t.Fatal("enum values shared mutable storage")
	}
	if _, err := models.ParseStatus("invalid"); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid enum accepted")
	}
	if _, err := json.Marshal(models.Status("invalid")); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid enum serialized")
	}
	for _, input := range []string{`null`, `"invalid"`, `42`} {
		v := models.StatusActive
		if err := json.Unmarshal([]byte(input), &v); !errors.Is(err, fault.Invalid) || v != models.StatusActive {
			t.Fatal("enum decoding lost validation/ownership")
		}
	}
	level, err := models.ParseLevel("2")
	if err != nil || level != models.LevelAdvanced {
		t.Fatal("integer enum failed")
	}
	if _, err := models.ParseLevel("258"); !errors.Is(err, fault.Invalid) {
		t.Fatal("integer enum overflow accepted")
	}
	data, err := json.Marshal(level)
	if err != nil || string(data) != "2" {
		t.Fatal("integer enum wire type changed")
	}
}
