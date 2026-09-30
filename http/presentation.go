package http

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func scalarPresentation(name string, p contract.Presentation, describe func() (URLScalarInfo, error)) error {
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
	if err := p.Validate(); err != nil {
		return err
	}
	if p.ValidateType(scalar.Value) != nil {
		return fault.New(fault.Invalid, fmt.Sprintf("client presentation on parameter %q contradicts its codec", name))
	}
	return nil
}

// WithPresentation attaches bounded public display metadata to this binding.
// The codec and requiredness remain authoritative; contradictory hints fail
// registration. No submitted value is inspected or exported.
func (p QueryParameter[Q]) WithPresentation(presentation contract.Presentation) QueryParameter[Q] {
	if p.err == nil {
		p.err = scalarPresentation(p.info.Name, presentation, p.scalar)
	}
	p.info.Presentation = presentation
	return p
}

// urlPresentation checks a hint on a URL component. Credentials must not travel
// in URLs, which proxies, logs and browser history retain.
func urlPresentation(p contract.Presentation) error {
	if p.Kind == contract.PasswordPresentation {
		return fault.New(fault.Invalid, "password presentation cannot describe a URL parameter")
	}
	return nil
}

// urlPresentation checks this declaration as an endpoint's URL query. The same
// parameters remain valid urlencoded form body fields.
func (d Query[Q]) urlPresentation() error {
	for _, parameter := range d.parameters {
		if err := urlPresentation(parameter.info.Presentation); err != nil {
			return err
		}
	}
	return nil
}

// outputPresentation rejects a JSON or event-stream payload whose graph reaches
// a password hint. Password hints are input-only; a response declares an
// explicit view. Route registration checks it once; link generation does not.
func (r Response[R]) outputPresentation() error {
	var describe func() (contract.Schema, error)
	switch r.kind {
	case payloadJSON:
		describe = r.json.Description
	case payloadEvents:
		describe = r.events.Description
	default:
		return nil
	}
	schema, err := describe()
	if err != nil {
		return err
	}
	if contract.PasswordTypes(schema.Types)[schema.Root] {
		return fault.New(fault.Invalid, "password presentation is input-only; declare an explicit response view")
	}
	return nil
}

// WithPresentation attaches public hints to a typed path field. Path segments
// are URL components, so password hints are rejected.
func (p PathParameter[P]) WithPresentation(presentation contract.Presentation) PathParameter[P] {
	if p.err == nil {
		p.err = urlPresentation(presentation)
	}
	if p.err == nil {
		p.err = scalarPresentation(p.name, presentation, p.scalar)
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
