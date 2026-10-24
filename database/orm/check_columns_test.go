package orm_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type invalidCheckedEnum int

func (invalidCheckedEnum) EnumDescriptor() enum.Descriptor[invalidCheckedEnum] {
	return enum.Descriptor[invalidCheckedEnum]{}
}

type panickingCheckedEnum int

func (panickingCheckedEnum) EnumDescriptor() enum.Descriptor[panickingCheckedEnum] {
	panic("private enum descriptor value")
}

type exitingCheckedEnum int

func (exitingCheckedEnum) EnumDescriptor() enum.Descriptor[exitingCheckedEnum] {
	runtime.Goexit()
	return enum.Descriptor[exitingCheckedEnum]{}
}

func TestCheckRetainsMixedColumnFailuresInOrder(t *testing.T) {
	type row struct {
		ID      int64
		Missing string
		Before  invalidCheckedEnum
		Panic   panickingCheckedEnum
		Exit    exitingCheckedEnum
		After   invalidCheckedEnum
	}
	type fields struct {
		orm.Table[row, int64]
		Before orm.Enum[row, invalidCheckedEnum]
		Panic  orm.Enum[row, panickingCheckedEnum]
		Exit   orm.Enum[row, exitingCheckedEnum]
		After  orm.Enum[row, invalidCheckedEnum]
	}
	table := orm.Define("mixed_columns", func(b orm.Builder[row], m *row) fields {
		return fields{orm.TableOf(b, &m.ID), orm.EnumOf(b, &m.Before), orm.EnumOf(b, &m.Panic), orm.EnumOf(b, &m.Exit), orm.EnumOf(b, &m.After)}
	})
	var previous string
	for range 2 {
		err := orm.Check(table.Table)
		if !errors.Is(err, fault.Invalid) || !errors.Is(err, fault.Panicked) {
			t.Fatal("mixed declaration failures lost classification", err)
		}
		message := err.Error()
		last := -1
		for _, name := range []string{"Missing", "mixed_columns.Before", "mixed_columns.Panic", "mixed_columns.Exit", "mixed_columns.After"} {
			index := strings.Index(message, name)
			if index <= last {
				t.Fatalf("missing or out-of-order column diagnostic %q: %s", name, message)
			}
			last = index
		}
		if strings.Contains(message, "private enum descriptor value") {
			t.Fatal("panic value disclosed")
		}
		if previous != "" && message != previous {
			t.Fatal("cached validation lost earlier column failures")
		}
		previous = message
	}
}
