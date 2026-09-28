package consumer_test

import (
	"context"
	"reflect"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type contribution struct {
	Name  string
	Value int
}
type assembledFeatures struct{ Contributions []contribution }

func TestTypedProviderContributions(t *testing.T) {
	base := foundation.NewKey[int]("base")
	first, second := foundation.NewKey[contribution]("first"), foundation.NewKey[contribution]("second")
	features := foundation.NewKey[assembledFeatures]("features")
	app, err := foundry.New().Register(
		foundation.Module{Name: "assembly", OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, features, func(s foundation.Resolver) (assembledFeatures, error) {
				items, err := foundation.ResolveAll[contribution](s)
				return assembledFeatures{Contributions: items}, err
			})
		}},
		foundation.Module{Name: "second", Requires: []foundation.ProviderID{"first"}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, second, func(s foundation.Resolver) (contribution, error) {
				value, err := foundation.Resolve(s, base)
				return contribution{Name: "second", Value: value + 1}, err
			})
		}},
		foundation.Module{Name: "first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, base, 10); err != nil {
				return err
			}
			return foundation.Provide(r, first, contribution{Name: "first", Value: 10})
		}},
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	got, err := foundation.Resolve(app.Services(), features)
	if err != nil || !reflect.DeepEqual(got.Contributions, []contribution{{Name: "first", Value: 10}, {Name: "second", Value: 11}}) {
		t.Fatalf("typed contribution assembly: %v %v", got, err)
	}
}
