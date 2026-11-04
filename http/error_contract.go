package http

import (
	"reflect"
	"sync"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/contractmeta"
)

// ErrorResponseJSON describes the actual shared failure envelope. Its fields
// and tags remain owned by ErrorResponse and contract.Issue; exporters obtain
// status/code membership separately from the router's error definitions.
func ErrorResponseJSON() contract.JSON[ErrorResponse] { return errorResponseDescriptor() }

// Every detailed failure measures this envelope, so it compiles once. A
// descriptor is immutable and its Description returns an owned snapshot.
var errorResponseDescriptor = sync.OnceValue(func() contract.JSON[ErrorResponse] {
	root := reflect.TypeFor[ErrorResponse]()
	schema, err := contractmeta.Struct(root, []reflect.Type{root, reflect.TypeFor[contract.Issue]()}, nil)
	if err != nil {
		return contract.JSON[ErrorResponse]{}
	}
	return contract.DefineJSON[ErrorResponse](schema)
})
