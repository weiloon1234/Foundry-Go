package invalid

import (
	"github.com/weiloon1234/Foundry-Go/application"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func wrong(builder *application.Builder) {
	builder.Fallback(func(application.Services) ([]h.RouteRegistration, error) { return nil, nil })
}
