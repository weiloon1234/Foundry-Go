// Package validationinput verifies typed validation from a separate consumer.
// These explicit field constructors are the generator's runtime boundary;
// automatic rule discovery remains part of the validation milestone.
package validationinput

import "github.com/weiloon1234/Foundry-Go/validation"

type Request struct {
	Name string
	Age  uint16
}

var Name = validation.DefineField("displayName", func(input Request) string { return input.Name })
var Age = validation.DefineField("age", func(input Request) uint16 { return input.Age })

var Rules = validation.All(
	Name.Rules(validation.Bail(validation.NonBlank[string](), validation.MinLength[string](3))),
	Age.Rules(validation.Min[uint16](18), validation.Max[uint16](120)),
)
