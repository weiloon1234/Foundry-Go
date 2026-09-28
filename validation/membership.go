package validation

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// OneOf accepts one of the declared scalar values without coercion. Input and
// declaration values retain their concrete type; empty/duplicate lists fail.
func OneOf[K Scalar](values ...K) Rule[K] { return scalarMembership(false, values) }

// NotOneOf rejects any declared scalar value. Non-finite numbers and invalid
// UTF-8 are rejected even when they are absent from the exclusion list.
func NotOneOf[K Scalar](values ...K) Rule[K] { return scalarMembership(true, values) }

func scalarMembership[K Scalar](exclude bool, values []K) Rule[K] {
	if len(values) == 0 || len(values) >= maxRuleNodes {
		return failed[K](invalid("membership requires a bounded nonempty value list"))
	}
	wire := make([]json.RawMessage, 0, len(values))
	set := make(map[K]struct{}, len(values))
	size := 2
	for _, item := range values {
		state := execution{limits: Limits{ValueBytes: maxDeclarationText}}
		if !scalarValue(&state, item) {
			return failed[K](invalid("invalid membership value"))
		}
		if _, duplicate := set[item]; duplicate {
			return failed[K](invalid("duplicate membership value"))
		}
		set[item] = struct{}{}
		data, err := json.Marshal(scalarPrimitive(item))
		if err != nil || len(data)+1 > maxDeclarationText-size {
			return failed[K](invalid("membership metadata exceeds its byte bound"))
		}
		size += len(data) + 1
		wire = append(wire, data)
	}
	spec := Spec{ID: "foundry.one_of", Parameters: []Parameter{parameter("values", wire)}}
	if exclude {
		spec.ID = "foundry.not_one_of"
	}
	return membershipRule(spec, set, exclude)
}

func membershipRule[K Scalar](spec Spec, set map[K]struct{}, exclude bool) Rule[K] {
	return valueRule(spec, false, func(s *execution, input K) (bool, error) {
		if !scalarValue(s, input) {
			return false, nil
		}
		_, found := set[input]
		return found != exclude, nil
	})
}

// Enum validates a concrete enum from its existing descriptor. Runtime values
// and actual wire cases share that descriptor; custom enum codecs are owned at
// construction and do not execute on each input check.
func Enum[E enum.Scalar](descriptor enum.Descriptor[E]) Rule[E] {
	cases := descriptor.Cases()
	if len(cases) == 0 || len(cases) >= maxRuleNodes {
		return failed[E](invalid("enum validation requires bounded cases"))
	}
	var definition enum.Definition
	var definitionErr error
	ownedErr := callback.Isolated("validation enum definition", func() error {
		definition, definitionErr = descriptor.Definition()
		return nil
	})
	if ownedErr != nil {
		return failed[E](fault.Wrap(fault.Invalid, "enum validation definition failed", ownedErr))
	}
	if definitionErr != nil {
		return failed[E](fault.Wrap(fault.Invalid, "invalid enum validation definition", definitionErr))
	}
	set := make(map[E]struct{}, len(cases))
	for _, item := range cases {
		set[item.Value] = struct{}{}
	}
	wire := make([]json.RawMessage, len(definition.Cases))
	size := 2
	for i, item := range definition.Cases {
		if len(item.Value)+1 > maxDeclarationText-size {
			return failed[E](invalid("enum validation metadata exceeds its byte bound"))
		}
		size += len(item.Value) + 1
		wire[i] = item.Value
	}
	rule := membershipRule(Spec{ID: "foundry.enum", Parameters: []Parameter{
		parameter("enum", definition.PackagePath+"."+definition.Name), parameter("values", wire),
	}}, set, false)
	// Enum membership uses the descriptor's validated actual wire cases.
	if rule.err == nil {
		rule.info.ServerOnly = false
	}
	return rule
}
