package foundation_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestConstructorGoexitReportsFailureAndExpiresResolver(t *testing.T) {
	base, exiting := foundation.NewKey[int]("base"), foundation.NewKey[string]("exiting")
	var retained foundation.Resolver
	var buildErr error
	returned, booted := false, false
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, buildErr = foundation.NewBuilder().Register(foundation.Module{Name: "exit", OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }, OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, base, 1); err != nil {
				return err
			}
			return foundation.Factory(r, exiting, func(s foundation.Resolver) (string, error) { retained = s; runtime.Goexit(); return "", nil })
		}}).Build(t.Context())
		returned = true
	}()
	<-done
	if !returned || !errors.Is(buildErr, fault.Panicked) || booted {
		t.Errorf("constructor exit escaped Build: returned=%v error=%v booted=%v", returned, buildErr, booted)
	}
	if retained == nil {
		t.Fatal("constructor never ran")
	}
	if _, err := foundation.Resolve(retained, base); !errors.Is(err, fault.Closed) {
		t.Errorf("constructor exit retained single resolution: %v", err)
	}
	if _, err := foundation.ResolveAll[int](retained); !errors.Is(err, fault.Closed) {
		t.Errorf("constructor exit retained collection resolution: %v", err)
	}
}

func TestResolveAllConstructsContributionsOnceInProviderOrder(t *testing.T) {
	first, second := foundation.NewKey[int]("first"), foundation.NewKey[int]("second")
	list := foundation.NewKey[[]int]("assembled")
	var count atomic.Int32
	var captured foundation.Resolver
	app := build(t,
		foundation.Module{Name: "assemble", OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, list, func(s foundation.Resolver) ([]int, error) { captured = s; return foundation.ResolveAll[int](s) })
		}},
		foundation.Module{Name: "second", Requires: []foundation.ProviderID{"first"}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, second, func(s foundation.Resolver) (int, error) {
				count.Add(1)
				value, err := foundation.Resolve(s, first)
				return value + 1, err
			})
		}},
		foundation.Module{Name: "first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, foundation.NewKey[string]("unrelated"), "ignored"); err != nil {
				return err
			}
			return foundation.Factory(r, first, func(foundation.Resolver) (int, error) { count.Add(1); return 1, nil })
		}},
	)
	got, err := foundation.Resolve(app.Services(), list)
	if err != nil || !reflect.DeepEqual(got, []int{1, 2}) || count.Load() != 2 {
		t.Fatalf("collection lost order or reconstructed services: %v %v", got, err)
	}
	if _, err := foundation.ResolveAll[int](captured); !errors.Is(err, fault.Closed) {
		t.Fatal("escaped constructor resolver was usable")
	}
	got, err = foundation.ResolveAll[int](app.Services())
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 99
	next, err := foundation.ResolveAll[int](app.Services())
	if err != nil || !reflect.DeepEqual(next, []int{1, 2}) {
		t.Fatal("returned slice mutated service collection")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			items, err := foundation.ResolveAll[int](app.Services())
			if err != nil || !reflect.DeepEqual(items, []int{1, 2}) {
				t.Error("concurrent service collection changed")
			}
		})
	}
	wg.Wait()
}

func TestResolveAllUsesExactDeclaredTypesAndAllowsEmpty(t *testing.T) {
	type named int
	app := build(t, foundation.Module{Name: "types", OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Provide(r, foundation.NewKey[named]("named"), named(1)); err != nil {
			return err
		}
		if err := foundation.Provide(r, foundation.NewKey[any]("explicit-interface"), any(2)); err != nil {
			return err
		}
		return foundation.Provide(r, foundation.NewKey[int]("plain"), 3)
	}})
	plain, err := foundation.ResolveAll[int](app.Services())
	if err != nil || !reflect.DeepEqual(plain, []int{3}) {
		t.Fatal("collection selected an assignable or underlying type")
	}
	interfaces, err := foundation.ResolveAll[any](app.Services())
	if err != nil || !reflect.DeepEqual(interfaces, []any{2}) {
		t.Fatal("interface collection included undeclared concrete services")
	}
	empty, err := foundation.ResolveAll[string](app.Services())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("absent contribution type was not an empty collection")
	}
	if _, err := foundation.ResolveAll[int](nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil resolver accepted")
	}
}

func TestResolveAllCyclesAndFailuresPrecedeBoot(t *testing.T) {
	for _, mode := range []string{"self", "indirect", "missing", "failure"} {
		t.Run(mode, func(t *testing.T) {
			a, b := foundation.NewKey[int]("a"), foundation.NewKey[[]int]("b")
			want := error(fault.Cycle)
			if mode == "missing" {
				want = fault.Missing
			}
			if mode == "failure" {
				want = errors.New("constructor veto")
			}
			var booted bool
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "test", OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }, OnRegister: func(r *foundation.Registrar) error {
				if err := foundation.Factory(r, b, func(s foundation.Resolver) ([]int, error) { return foundation.ResolveAll[int](s) }); err != nil {
					return err
				}
				return foundation.Factory(r, a, func(s foundation.Resolver) (int, error) {
					switch mode {
					case "self":
						_, err := foundation.ResolveAll[int](s)
						return 0, err
					case "indirect":
						_, err := foundation.Resolve(s, b)
						return 0, err
					case "missing":
						return foundation.Resolve(s, foundation.NewKey[int]("absent"))
					default:
						return 0, want
					}
				})
			}}).Build(t.Context())
			if !errors.Is(err, want) || booted {
				t.Fatalf("invalid collection published a bootable app: %v", err)
			}
		})
	}
}
