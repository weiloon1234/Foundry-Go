package ormcore

import (
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/value"
)

// EncryptedType recognizes the encrypted.Value contract without invoking user
// methods. Nullable wrappers retain the restriction of their underlying value.
func EncryptedType(typ reflect.Type) bool {
	nullable := reflect.TypeFor[value.Nullable[int]]()
	for typ.PkgPath() == nullable.PkgPath() && strings.HasPrefix(typ.Name(), "Nullable[") {
		get, ok := typ.MethodByName("Get")
		if !ok {
			return false
		}
		typ = get.Type.Out(0)
	}
	contract := reflect.TypeFor[encrypted.Value[encrypted.Text]]()
	example := reflect.TypeFor[encrypted.Text]()
	seal, ok := typ.MethodByName("Seal")
	if !ok || seal.Type.NumOut() != 2 {
		return false
	}
	result := seal.Type.Out(0)
	for i := 0; i < contract.NumMethod(); i++ {
		expected := contract.Method(i)
		actual, ok := typ.MethodByName(expected.Name)
		if !ok {
			return false
		}
		offset := 1
		if typ.Kind() == reflect.Interface {
			offset = 0
		}
		if actual.Type.NumIn() != expected.Type.NumIn()+offset || actual.Type.NumOut() != expected.Type.NumOut() {
			return false
		}
		replace := func(t reflect.Type) reflect.Type {
			if t == example {
				return result
			}
			return t
		}
		for j := 0; j < expected.Type.NumIn(); j++ {
			if actual.Type.In(j+offset) != replace(expected.Type.In(j)) {
				return false
			}
		}
		for j := 0; j < expected.Type.NumOut(); j++ {
			if actual.Type.Out(j) != replace(expected.Type.Out(j)) {
				return false
			}
		}
	}
	return true
}
