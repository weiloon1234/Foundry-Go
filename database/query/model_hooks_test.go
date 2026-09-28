package query

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestModelHookFactoryRunsInsideWriteAndAfterShapeValidation(t *testing.T) {
	base := mutatorQuery(func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil })
	factories, beforeCalls, begins := 0, 0, 0
	active := false
	veto := errors.New("before veto")
	definition := base.definition.WithWriteHooks(func() WriteHooks[mutatorRecord] {
		factories++
		if !active {
			t.Error("hook factory ran outside the transaction")
		}
		return WriteHooks[mutatorRecord]{Before: func(ctx context.Context, tx *database.Tx, op lifecycle.Operation, before value.Optional[mutatorRecord], m Mutation[mutatorRecord]) (Mutation[mutatorRecord], error) {
			beforeCalls++
			if op != lifecycle.Create || before.IsSet() {
				t.Error("create hook received wrong operation/snapshot")
			}
			return Mutation[mutatorRecord]{}, veto
		}}
	})
	q := ForModel(definition)
	if _, err := q.Compile(); err != nil {
		t.Fatal(err)
	}
	if factories != 0 {
		t.Fatal("building or compiling a read invoked hooks")
	}
	writer := mutatorTransactor(func(ctx context.Context, work func(*database.Tx) error) error {
		begins++
		active = true
		defer func() { active = false }()
		return work(nil)
	})
	// A hook may supply omitted required fields, so required-value validation
	// follows Before. The sentinel ensures this test never needs a connection.
	if _, err := q.Insert(t.Context(), writer, Change[mutatorRecord]()); !errors.Is(err, veto) {
		t.Fatalf("missing field was rejected before the hook: %v", err)
	}
	name := Assign[mutatorRecord]("records", "name", codec.String[string](), "value")
	for _, mutation := range []Mutation[mutatorRecord]{Change(name, name), Change(Assign[mutatorRecord]("wrong", "name", codec.String[string](), "x"))} {
		if _, err := q.Insert(t.Context(), writer, mutation); !errors.Is(err, fault.Invalid) {
			t.Fatalf("shape mismatch passed to hooks: %v", err)
		}
	}
	if factories != 1 || beforeCalls != 1 || begins != 1 {
		t.Fatalf("invalid shape began a write or hook retried: %d/%d/%d", factories, beforeCalls, begins)
	}
	if err := base.definition.WithWriteHooks(nil).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil factory silently disabled hooks")
	}
	if base.definition.hasWriteHooks {
		t.Fatal("definition hook registration mutated original metadata")
	}
}

func TestModelHookNestingBoundPreservesContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "origin")
	for i := 0; i < MaxLifecycleDepth; i++ {
		var err error
		ctx, err = writeHookContext(ctx)
		if err != nil || ctx.Value(key{}) != "origin" {
			t.Fatal("valid nested write lost context")
		}
	}
	if _, err := writeHookContext(ctx); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded recursive write accepted")
	}
	if _, err := writeHookContext(t.Context()); err != nil {
		t.Fatal("depth leaked between operations")
	}
}

func TestObserverAdapterMustBeValidAndUnstartedPoolIsNotFrozen(t *testing.T) {
	base := mutatorQuery(func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil })
	if err := base.definition.WithObserverHooks(nil, false).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil observer adapter silently disabled registrations", err)
	}
	if _, known := writerObservers(new(database.DB)); known {
		t.Fatal("an unstarted pool cannot prove its observer set is frozen")
	}
	if _, known := writerObservers(mutatorTransactor(nil)); known {
		t.Fatal("a wrapper cannot prove absence of observers")
	}
}
