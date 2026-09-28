package pagination

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/contractmeta"
	"github.com/weiloon1234/Foundry-Go/value"
)

// NumberedJSON composes an explicitly declared item DTO with the framework's
// numbered response. The template's Go fields/tags own its property names;
// item fields and codecs remain owned by the supplied typed JSON descriptor.
func NumberedJSON[T any](item contract.JSON[T]) contract.JSON[NumberedResponse[T]] {
	return pageJSON[NumberedResponse[T]](item)
}

// SimpleJSON retains the count-free response shape and the concrete item DTO.
func SimpleJSON[T any](item contract.JSON[T]) contract.JSON[SimpleResponse[T]] {
	return pageJSON[SimpleResponse[T]](item)
}

// CursorJSON composes an explicit item DTO with count-free cursor metadata.
func CursorJSON[T any](item contract.JSON[T]) contract.JSON[CursorResponse[T]] {
	return pageJSON[CursorResponse[T]](item)
}

func pageJSON[Envelope, T any](item contract.JSON[T]) contract.JSON[Envelope] {
	invalid := func() contract.JSON[Envelope] { return contract.DefineJSON[Envelope](contract.Schema{}) }
	itemInfo, err := item.Description()
	if err != nil {
		return invalid()
	}
	root := reflect.TypeFor[Envelope]()
	extras := map[reflect.Type]contract.Type{
		reflect.TypeFor[[]T]():                    {ID: contract.TypeID("pagination:items:" + string(itemInfo.Root)), Kind: contract.ArrayKind, Element: itemInfo.Root},
		reflect.TypeFor[value.Nullable[string]](): {ID: contractmeta.TypeID(reflect.TypeFor[value.Nullable[string]]()), Kind: contract.AliasKind, Nullable: true, Element: "string"},
	}
	schema, err := contractmeta.Struct(root, []reflect.Type{root, reflect.TypeFor[NumberedMeta](), reflect.TypeFor[SimpleMeta](), reflect.TypeFor[Links](), reflect.TypeFor[CursorMeta]()}, extras,
		contract.JSONType(itemInfo.Root, func() contract.JSON[T] { return item }), contract.Type{ID: "string", Kind: contract.StringKind})
	if err != nil {
		return invalid()
	}
	return contract.DefineJSON[Envelope](schema)
}
