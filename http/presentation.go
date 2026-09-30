package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func scalarPresentation(p contract.Presentation, describe func() (URLScalarInfo, error)) error {
	if p == (contract.Presentation{}) {
		return nil
	}
	if describe == nil {
		return fault.New(fault.Invalid, "presentation requires a described scalar")
	}
	scalar, err := describe()
	if err != nil {
		return err
	}
	return p.ValidateType(scalar.Value)
}

// WithPresentation attaches bounded public display metadata to this binding.
// The codec and requiredness remain authoritative; contradictory hints fail
// registration. No submitted value is inspected or exported.
func (p QueryParameter[Q]) WithPresentation(presentation contract.Presentation) QueryParameter[Q] {
	if p.err == nil {
		p.err = scalarPresentation(presentation, p.scalar)
	}
	p.info.Presentation = presentation
	return p
}

// WithPresentation attaches public hints to a typed path field.
func (p PathParameter[P]) WithPresentation(presentation contract.Presentation) PathParameter[P] {
	if p.err == nil {
		p.err = scalarPresentation(presentation, p.scalar)
	}
	p.presentation = presentation
	return p
}

// WithPresentation attaches public hints to the declared multipart value.
// Only file parts accept FilePresentation; JSON parts retain their own schema.
func (p MultipartPart[B]) WithPresentation(presentation contract.Presentation) MultipartPart[B] {
	if p.err != nil {
		return p
	}
	switch p.kind {
	case MultipartText:
		p.text = p.text.WithPresentation(presentation)
		p.err = p.text.err
	case MultipartFile:
		p.err = presentation.ValidateFile()
	case MultipartJSON:
		if p.schema == nil {
			p.err = fault.New(fault.Invalid, "presentation requires a JSON part schema")
			break
		}
		schema, err := p.schema()
		if err != nil {
			p.err = err
		} else {
			p.err = presentation.ValidateSchema(schema)
		}
	default:
		p.err = fault.New(fault.Invalid, "presentation requires a multipart kind")
	}
	p.info.Presentation = presentation
	return p
}
