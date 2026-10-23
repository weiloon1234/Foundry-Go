package modelmeta

import (
	"reflect"
	"testing"
)

type methodModel struct{ Email string }

func (methodModel) MutateEmail(v int) (string, error) { panic("must not run during discovery") }
func (methodModel) AccessEmail() (int, error)         { panic("must not run during discovery") }
func TestFieldMethodsBindsWithoutInvocation(t *testing.T) {
	methods, err := FieldMethods(reflect.TypeFor[methodModel](), map[string]reflect.Type{"Email": reflect.TypeFor[string]()})
	if err != nil || len(methods) != 2 {
		t.Fatal("method binding", err)
	}
	for _, m := range methods {
		if m.Prefix == "Mutate" {
			if _, ok := m.Bound.(func(methodModel, int) (string, error)); !ok {
				t.Fatal("lost concrete signature")
			}
		}
	}
}
