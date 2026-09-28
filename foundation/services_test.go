package foundation_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestTypedServiceGraphResolvesOnceAndFreezes(t *testing.T) {
	base := foundation.NewKey[int]("base")
	derived := foundation.NewKey[string]("derived")
	var count atomic.Int32
	var retained *foundation.Registrar
	var construction foundation.Resolver
	app := build(t, foundation.Module{Name: "one", OnRegister: func(r *foundation.Registrar) error {
		retained = r
		if err := foundation.Factory(r, derived, func(resolver foundation.Resolver) (string, error) {
			construction = resolver
			value, err := foundation.Resolve(resolver, base)
			if err != nil {
				return "", err
			}
			if value != 42 {
				return "", errors.New("wrong dependency")
			}
			return "ready", nil
		}); err != nil {
			return err
		}
		return foundation.Factory(r, base, func(foundation.Resolver) (int, error) { count.Add(1); return 42, nil })
	}})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			value, err := foundation.Resolve(app.Services(), derived)
			if err != nil || value != "ready" {
				t.Errorf("value=%q error=%v", value, err)
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("constructed %d times", count.Load())
	}
	if err := foundation.Provide(retained, base, 55); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	if _, err := foundation.Resolve(construction, base); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	if _, err := foundation.Resolve(app.Services(), foundation.NewKey[string]("base")); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestServiceGraphErrorsPrecedeBoot(t *testing.T) {
	a, b := foundation.NewKey[int]("a"), foundation.NewKey[int]("b")
	for _, test := range []struct {
		name     string
		register func(*foundation.Registrar) error
		code     fault.Code
	}{
		{"missing", func(r *foundation.Registrar) error {
			return foundation.Factory(r, a, func(s foundation.Resolver) (int, error) { return foundation.Resolve(s, b) })
		}, fault.Missing},
		{"cycle", func(r *foundation.Registrar) error {
			if err := foundation.Factory(r, a, func(s foundation.Resolver) (int, error) { return foundation.Resolve(s, b) }); err != nil {
				return err
			}
			return foundation.Factory(r, b, func(s foundation.Resolver) (int, error) { return foundation.Resolve(s, a) })
		}, fault.Cycle},
		{"duplicate", func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, a, 1); err != nil {
				return err
			}
			return foundation.Provide(r, a, 2)
		}, fault.Duplicate},
		{"nil", func(r *foundation.Registrar) error {
			return foundation.Provide(r, foundation.NewKey[*int]("ptr"), (*int)(nil))
		}, fault.Invalid},
		{"panic", func(r *foundation.Registrar) error {
			return foundation.Factory(r, a, func(foundation.Resolver) (int, error) { panic("secret") })
		}, fault.Panicked},
	} {
		t.Run(test.name, func(t *testing.T) {
			var booted atomic.Bool
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "test", OnRegister: test.register, OnBoot: func(context.Context, *foundation.Runtime) error { booted.Store(true); return nil }}).Build(t.Context())
			if !errors.Is(err, test.code) || booted.Load() {
				t.Fatalf("error=%v booted=%v", err, booted.Load())
			}
		})
	}
}

func TestIndependentAppsDoNotShareServiceBindings(t *testing.T) {
	key := foundation.NewKey[*atomic.Int32]("counter")
	provider := foundation.Module{Name: "counter", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (*atomic.Int32, error) { return &atomic.Int32{}, nil })
	}}
	first, second := build(t, provider), build(t, provider)
	a, err := foundation.Resolve(first.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := foundation.Resolve(second.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	a.Add(1)
	if a == b || b.Load() != 0 {
		t.Fatal("application services leaked across instances")
	}
}
