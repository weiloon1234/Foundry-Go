// Package validationinput verifies typed validation from a separate consumer.
// These explicit field constructors use the same runtime as generated rules.
package validationinput

import "github.com/weiloon1234/Foundry-Go/validation"

type Request struct {
	Name string
	Age  uint16
}

var Name = validation.DefineField("displayName", func(input Request) string { return input.Name })
var Age = validation.DefineField("age", func(input Request) uint16 { return input.Age })

var Rules = validation.All(
	validation.Rules(Name, validation.Bail(validation.NonBlank[string](), validation.MinLength[string](3))),
	validation.Rules(Age, validation.Min[uint16](18), validation.Max[uint16](120)),
)
