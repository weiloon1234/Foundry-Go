package http

import "github.com/weiloon1234/Foundry-Go/contract"

// PathParameterInfo retains pattern order, catch-all semantics and an optional
// codec-owned scalar description. Nil Scalar identifies an undescribed custom
// codec; exporters must report that gap instead of inferring a string schema.
type PathParameterInfo struct {
	Presentation contract.Presentation `json:"presentation,omitzero"`
	Name         string                `json:"name"`
	CatchAll     bool                  `json:"catch_all"`
	Scalar       *URLScalarInfo        `json:"scalar,omitempty"`
}

// Parameters returns an owned description from the same path and field bindings
// used at execution. It does not inspect a request or call Parse/Format.
func (p Path[P]) Parameters() ([]PathParameterInfo, error) {
	segments, err := p.validate()
	if err != nil {
		return nil, err
	}
	parameters := make([]PathParameterInfo, 0, len(p.parameters))
	for _, segment := range segments {
		if segment.Name == "" {
			continue
		}
		info := PathParameterInfo{Name: segment.Name, CatchAll: segment.Tail}
		for _, binding := range p.parameters {
			if binding.name == segment.Name && binding.scalar != nil {
				scalar, err := binding.scalar()
				if err != nil {
					return nil, err
				}
				info.Scalar = &scalar
				info.Presentation = binding.presentation
				break
			}
		}
		parameters = append(parameters, info)
	}
	return parameters, nil
}

// Parameters exposes typed path metadata for either typed or raw routes.
// Raw payloads do not acquire inferred request/response DTOs.
func (r Route[P]) Parameters() ([]PathParameterInfo, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r.path.Parameters()
}
