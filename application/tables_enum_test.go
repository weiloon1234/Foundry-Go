package application_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateEnumError struct{ message string }

func (e *privateEnumError) Error() string { return e.message }

var enumSentinel = &privateEnumError{message: "private-enum-marshaler-value"}

type failingStartupEnum int

func (failingStartupEnum) MarshalJSON() ([]byte, error) { return nil, enumSentinel }
func (failingStartupEnum) EnumDescriptor() enum.Descriptor[failingStartupEnum] {
	return enum.Describe("application.test", "StartupEnum", enum.Case[failingStartupEnum]{Name: "One", Value: 1})
}

func TestApplicationTablesRedactEnumMarshalerErrors(t *testing.T) {
	type row struct {
		ID     int64
		Status failingStartupEnum
	}
	type table struct {
		orm.Table[row, int64]
		Status orm.Enum[row, failingStartupEnum]
	}
	fields := orm.Define("startup_enums", func(b orm.Builder[row], m *row) table {
		return table{orm.TableOf(b, &m.ID), orm.EnumOf(b, &m.Status)}
	})
	app, err := application.New(settings(), quiet(), application.WithTables(fields.Table)).Build(t.Context())
	if app != nil || err == nil {
		t.Fatal("invalid enum declaration accepted")
	}
	var private *privateEnumError
	var marshaler *json.MarshalerError
	if !errors.Is(err, enumSentinel) || !errors.Is(err, fault.Invalid) || !errors.As(err, &private) || private != enumSentinel || !errors.As(err, &marshaler) {
		t.Fatal("startup error lost its enum cause or classification")
	}
	for _, message := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Errorf("startup failed: %w", err).Error()} {
		if strings.Contains(message, enumSentinel.message) {
			t.Fatal("startup diagnostic disclosed the marshaler's private data")
		}
	}
}
