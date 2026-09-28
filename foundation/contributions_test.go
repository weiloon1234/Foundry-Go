package foundation_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestTypedCollectionsOnlyResolveSelectedScope(t *testing.T) {
	one := foundation.NewCollection[string]("one")
	two := foundation.NewCollection[string]("two")
	first := foundation.NewKey[[]string]("first")
	second := foundation.NewKey[[]string]("second")
	app := build(t, foundation.Module{Name: "collections", OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, first, func(resolver foundation.Resolver) ([]string, error) { return foundation.Contributions(resolver, one) }); err != nil {
			return err
		}
		if err := foundation.Factory(r, second, func(resolver foundation.Resolver) ([]string, error) { return foundation.Contributions(resolver, two) }); err != nil {
			return err
		}
		if err := foundation.Contribute(r, one, "entry", func(foundation.Resolver) (string, error) { return "first", nil }); err != nil {
			return err
		}
		return foundation.Contribute(r, two, "entry", func(resolver foundation.Resolver) (string, error) {
			values, err := foundation.Resolve(resolver, first)
			if err != nil {
				return "", err
			}
			return values[0] + " second", nil
		})
	}})
	values, err := foundation.Resolve(app.Services(), second)
	if err != nil || !slices.Equal(values, []string{"first second"}) {
		t.Fatalf("scope crossed into unrelated constructor: %v %v", values, err)
	}
	values, err = foundation.Contributions(app.Services(), two)
	if err != nil || !slices.Equal(values, []string{"first second"}) {
		t.Fatalf("resolved collection: %v %v", values, err)
	}
	values[0] = "corrupt"
	unchanged, _ := foundation.Contributions(app.Services(), two)
	if unchanged[0] != "first second" {
		t.Fatal("collection shares result slice")
	}
}

func TestContributionOverrideRetainsErasedSchemaAndPosition(t *testing.T) {
	type payloadA struct{ Value string }
	type payloadB struct{ Value string }
	collection := foundation.NewCollection[string]("jobs")
	base := foundation.Module{Name: "base", OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.ContributeAs[payloadA](r, collection, "first", func(foundation.Resolver) (string, error) { return "original", nil }); err != nil {
			return err
		}
		return foundation.Contribute(r, collection, "second", func(foundation.Resolver) (string, error) { return "second", nil })
	}}
	_, err := foundation.NewBuilder().Register(base).OverrideContributions("application", func(r *foundation.Registrar) error {
		return foundation.ContributeAs[payloadB](r, collection, "first", func(foundation.Resolver) (string, error) { return "wrong schema", nil })
	}).Build(t.Context())
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("different schema crossed erased boundary", err)
	}
	app, err := foundation.NewBuilder().Register(base).OverrideContributions("application", func(r *foundation.Registrar) error {
		return foundation.ContributeAs[payloadA](r, collection, "first", func(foundation.Resolver) (string, error) { return "override", nil })
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values, err := foundation.Contributions(app.Services(), collection)
	if err != nil || !slices.Equal(values, []string{"override", "second"}) {
		t.Fatalf("override moved declaration: %v %v", values, err)
	}
}
