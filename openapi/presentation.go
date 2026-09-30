package openapi

import "github.com/weiloon1234/Foundry-Go/contract"

func presentationSchema(schema object, hint contract.Presentation) object {
	if hint != (contract.Presentation{}) {
		schema["x-foundry-presentation"] = hint
	}
	if hint.Kind == contract.PasswordPresentation {
		schema["writeOnly"] = true
	}
	return schema
}
