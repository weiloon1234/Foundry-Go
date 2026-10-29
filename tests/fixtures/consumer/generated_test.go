package consumer_test

import "time"

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
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

func TestHandwrittenModelsAndAssignments(t *testing.T) {
	id, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fields := models.Users
	q := fields.Query().Where(orm.KeyEq(fields.ID, id), orm.Or(orm.TextContains(fields.Email, "@example.test"), orm.NullableTextIsNull(fields.Nickname)), orm.NumberGte(fields.Age, 18), orm.EnumEq(fields.Status, models.StatusActive)).OrderBy(orm.NumberDesc(fields.Age))
	if statement, err := orm.Compile(q); err != nil || !strings.Contains(statement.SQL(), `"users"`) {
		t.Fatalf("query declaration: %v", err)
	}
	if _, err := orm.Compile(models.Countries.Query().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("MY")))); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Compile(fields.Query().Where(orm.NullableColumnEq(fields.IntroducerID, id))); err != nil {
		t.Fatal("self-reference lost its owner")
	}
	var base []orm.Assignment[models.User]
	changed := append(base, userIdentityAssignment(id), orm.NumberSet(fields.Age, 0), orm.TextSet(fields.Email, ""), orm.NullableTextSetNull(fields.Nickname), orm.NullableColumnSet(fields.IntroducerID, id))
	if len(base) != 0 || len(changed) != 5 {
		t.Fatal("assignment derivation mutated original")
	}
	inspectInsertAssignments(t, fields.Table, changed, func(write *orm.Write[models.User]) {
		if v, ok := orm.WriteGet(write, func() func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
			receiver := fields.ID
			return func(arg0 model.ID[models.User]) orm.Assignment[models.User] { return orm.KeySet(receiver, arg0) }
		}()).Get(); !ok || v != id {
			t.Error("typed identity input lost")
		}
		if v, ok := orm.WriteGet(write, func() func(arg0 int) orm.Assignment[models.User] {
			receiver := fields.Age
			return func(arg0 int) orm.Assignment[models.User] { return orm.NumberSet(receiver, arg0) }
		}()).Get(); !ok || v != 0 {
			t.Error("explicit zero lost")
		}
		if v, ok := orm.WriteGet(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Email
			return func(arg0 string) orm.Assignment[models.User] { return orm.TextSet(receiver, arg0) }
		}()).Get(); !ok || v != "" {
			t.Error("explicit empty string lost")
		}
		if v, ok := orm.WriteNullableGet(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Nickname
			return func(arg0 string) orm.Assignment[models.User] { return orm.NullableTextSet(receiver, arg0) }
		}()).Get(); !ok || !v.IsNull() {
			t.Error("explicit null lost")
		}
		if v, ok := orm.WriteNullableGet(write, func() func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
			receiver := fields.IntroducerID
			return func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
				return orm.NullableColumnSet(receiver, arg0)
			}
		}()).Get(); !ok || v != value.Of(id) {
			t.Error("typed self-reference input lost")
		}
		if err := orm.WriteUnset(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Nickname
			return func(arg0 string) orm.Assignment[models.User] { return orm.NullableTextSet(receiver, arg0) }
		}()); err != nil {
			t.Error(err)
		}
		if orm.WriteNullableGet(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Nickname
			return func(arg0 string) orm.Assignment[models.User] { return orm.NullableTextSet(receiver, arg0) }
		}()).IsSet() {
			t.Error("unset nickname remained supplied")
		}
	})
	inspectInsertAssignments(t, fields.Table, base, func(write *orm.Write[models.User]) {
		if orm.WriteIsSet(write, func() func(arg0 int) orm.Assignment[models.User] {
			receiver := fields.Age
			return func(arg0 int) orm.Assignment[models.User] { return orm.NumberSet(receiver, arg0) }
		}()) || orm.WriteIsSet(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Email
			return func(arg0 string) orm.Assignment[models.User] { return orm.TextSet(receiver, arg0) }
		}()) || orm.WriteIsSet(write, func() func(arg0 string) orm.Assignment[models.User] {
			receiver := fields.Nickname
			return func(arg0 string) orm.Assignment[models.User] { return orm.NullableTextSet(receiver, arg0) }
		}()) || orm.WriteIsSet(write, func() func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
			receiver := fields.IntroducerID
			return func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
				return orm.NullableColumnSet(receiver, arg0)
			}
		}()) {
			t.Error("assignment derivation populated the original input")
		}
	})
}

func TestBusinessAssignmentHelpers(t *testing.T) {
	for _, email := range []string{"typed", ""} {
		assignment := (models.User{Email: email}).CurrentEmailAssignment()
		inspectInsertAssignments(t, models.Users.Table, []orm.Assignment[models.User]{assignment}, func(write *orm.Write[models.User]) {
			if got, set := orm.WriteGet(write, func() func(arg0 string) orm.Assignment[models.User] {
				receiver := models.Users.Email
				return func(arg0 string) orm.Assignment[models.User] { return orm.TextSet(receiver, arg0) }
			}()).Get(); !set || got != email {
				t.Error("handwritten business method lost its supplied input")
			}
		})
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
