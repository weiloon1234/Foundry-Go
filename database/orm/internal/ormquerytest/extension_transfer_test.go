package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
)

func extensionTransferBinding(fetch extensionslot.Fetch[extensionModel, externalValue]) extensionslot.Binding[extensionModel, externalValue] {
	return extensionslot.Binding[extensionModel, externalValue]{Name: "External", Table: "adapter_owners", Get: func(m extensionModel) externalValue { return m.External }, Set: func(m extensionModel, v externalValue) extensionModel { m.External = v; return m }, Loaded: func(v externalValue) bool { return v.Loaded }, Count: func(v externalValue) int {
		if v.Loaded {
			return 1
		}
		return 0
	}, Fetch: fetch}
}
func TestExtensionSlotLoadsInRelationBatchesWithinTheBudget(t *testing.T) {
	d := extensionFields()
	var batches []int
	binding := extensionTransferBinding(func(_ context.Context, _ database.Executor, parents []extensionModel) ([]externalValue, error) {
		batches = append(batches, len(parents))
		out := make([]externalValue, len(parents))
		for i, p := range parents {
			out[i] = externalValue{Loaded: true, Value: strings.Repeat("x", int(p.ID))}
		}
		return out, nil
	})
	p := orm.FoundryExtension(d.External, extensionslot.New(binding), sqlowner.Queries())
	parents := []extensionModel{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	var sql []string
	db := hookDB(t, testConnector{statements: &sql})
	q := orm.WithRelationLimits(d.Query().With(p), orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 8})
	out, e := orm.LoadFrom(t.Context(), db, q, parents)
	if e != nil || !reflect.DeepEqual(batches, []int{2, 2, 1}) || out[4].External.Value != "xxxxx" || len(sql) != 0 {
		t.Fatal(batches, out, e)
	}
	batches = nil
	if _, e := orm.LoadMissingFrom(t.Context(), db, q, out); e != nil || len(batches) != 0 || len(sql) != 0 {
		t.Fatal("loaded slots refetched", batches, e)
	}
	batches = nil
	q = orm.WithRelationLimits(d.Query().With(p), orm.RelationLimits{BatchSize: 2, MaxRows: 3, MaxDepth: 8})
	out, e = orm.LoadFrom(t.Context(), db, q, parents)
	if !errors.Is(e, fault.Invalid) || !reflect.DeepEqual(batches, []int{2, 2}) || out != nil || parents[0].External.Loaded {
		t.Fatal("overflow leaked partial results or fetched too far", batches, out, e)
	}
}
func TestExtensionSlotValidationPrecedesLoading(t *testing.T) {
	d := extensionFields()
	fetched := false
	fetch := func(context.Context, database.Executor, []extensionModel) ([]externalValue, error) {
		fetched = true
		return nil, nil
	}
	valid := extensionTransferBinding(fetch)
	unbound := valid
	unbound.Fetch = nil
	unbound.Unbound = fault.New(fault.Missing, "slot unbound")
	foreign := valid
	foreign.Table = "other"
	incomplete := valid
	incomplete.Get = nil
	for name, test := range map[string]struct {
		binding        extensionslot.Binding[extensionModel, externalValue]
		zero, repeated bool
		want           error
	}{"unbound": {binding: unbound, want: fault.Missing}, "foreign": {binding: foreign, want: fault.Invalid}, "incomplete": {binding: incomplete, want: fault.Invalid}, "zero": {zero: true, want: fault.Invalid}, "misaligned": {binding: valid, want: fault.Invalid}, "repeated": {binding: valid, repeated: true, want: fault.Invalid}} {
		t.Run(name, func(t *testing.T) {
			fetched = false
			slot := extensionslot.New(test.binding)
			if test.zero {
				slot = extensionslot.Slot[extensionModel]{}
			}
			p := orm.FoundryExtension(d.External, slot, sqlowner.Queries())
			q := d.Query().With(p)
			if test.repeated {
				q = q.With(p)
			}
			_, e := orm.LoadFrom(t.Context(), hookDB(t, testConnector{}), q, []extensionModel{{ID: 1}})
			if !errors.Is(e, test.want) {
				t.Fatal(e)
			}
			if name != "misaligned" && fetched {
				t.Fatal("fetch before validation")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fetched = false
	p := orm.FoundryExtension(d.External, extensionslot.New(valid), sqlowner.Queries())
	if _, e := orm.LoadFrom(ctx, hookDB(t, testConnector{}), d.Query().With(p), []extensionModel{{ID: 1}}); !errors.Is(e, context.Canceled) || fetched {
		t.Fatal("cancelled fetch", e)
	}
}
