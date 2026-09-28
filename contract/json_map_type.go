package contract

import "reflect"

// JSONMapType is the generated map declaration boundary. M retains both key and
// value types; an unrelated JSONKey cannot bind to this map. IDs describe the
// generated map, its element and source key type respectively. Generation owns
// those source identities, including executable main-package namespaces.
func JSONMapType[M ~map[K]V, K comparable, V any](id, element, keyID TypeID, key JSONKey[K]) Type {
	typ := Type{ID: id, Kind: MapKind, Element: element, Nullable: true}
	if key.Validate() != nil || !declarationText(string(keyID)) {
		// An invalid descriptor must never degrade into an unconstrained map.
		typ.Kind = ""
		return typ
	}
	runtime := *key.runtime
	runtime.info = cloneJSONKeyInfo(runtime.info)
	runtime.info.Value.ID = keyID
	info := cloneJSONKeyInfo(runtime.info)
	typ.Key = &info
	typ.jsonKey = &runtime
	return typ
}

func validJSONMapKeyDeclaration(typ Type) bool {
	if typ.Key == nil && typ.jsonKey == nil {
		return true
	}
	if typ.Kind != MapKind || typ.Key == nil || typ.jsonKey == nil {
		return false
	}
	return reflect.DeepEqual(*typ.Key, typ.jsonKey.info)
}

func sameJSONKeyRuntime(left, right *jsonKeyRuntime) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	// The only runtime parser is the native checker for this exact type.
	// Shape, syntax and other rules are compared through public Key metadata.
	return left.typ == right.typ
}
