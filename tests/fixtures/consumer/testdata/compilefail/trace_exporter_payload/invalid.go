package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/observability"
)

var _ = observability.Config{TraceExporters: []observability.TraceExporter{func(context.Context, observability.ErrorReport) error { return nil }}}
